package availability

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"ispilolite/internal/models"
	"ispilolite/internal/utils"
)

var ErrInvalid = errors.New("invalid availability schedule")

type Repository interface {
	ReplaceRules(context.Context, string, []*models.AvailabilityRule) error
	Rules(context.Context, string) ([]*models.AvailabilityRule, error)
	UpsertException(context.Context, *models.AvailabilityException) error
	DeleteException(context.Context, string, string) error
	Exceptions(context.Context, string, time.Time, time.Time) ([]*models.AvailabilityException, error)
	BusyTimes(context.Context, string, time.Time, time.Time) ([]time.Time, error)
}

type Service struct{ repo Repository }

func NewService(repo Repository) *Service { return &Service{repo: repo} }

func (s *Service) Replace(ctx context.Context, technicianID string, rules []*models.AvailabilityRule) ([]*models.AvailabilityRule, error) {
	if len(rules) > 28 {
		return nil, ErrInvalid
	}
	now := time.Now().UTC()
	for _, rule := range rules {
		if rule == nil || rule.Weekday < 0 || rule.Weekday > 6 || !validClockRange(rule.StartTime, rule.EndTime) {
			return nil, ErrInvalid
		}
		if rule.Timezone == "" {
			rule.Timezone = "Africa/Nairobi"
		}
		if _, err := time.LoadLocation(rule.Timezone); err != nil {
			return nil, fmt.Errorf("%w: invalid timezone", ErrInvalid)
		}
		rule.ID, rule.TechnicianID, rule.Active, rule.CreatedAt = utils.GenerateID(), technicianID, true, now
	}
	// Reject overlapping rules on the same weekday.
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].Weekday == rules[j].Weekday {
			return rules[i].StartTime < rules[j].StartTime
		}
		return rules[i].Weekday < rules[j].Weekday
	})
	for i := 1; i < len(rules); i++ {
		if rules[i-1].Weekday == rules[i].Weekday && rules[i].StartTime < rules[i-1].EndTime {
			return nil, fmt.Errorf("%w: overlapping time ranges", ErrInvalid)
		}
	}
	if err := s.repo.ReplaceRules(ctx, technicianID, rules); err != nil {
		return nil, err
	}
	return rules, nil
}

func (s *Service) AddException(ctx context.Context, technicianID string, item *models.AvailabilityException) (*models.AvailabilityException, error) {
	if item == nil {
		return nil, ErrInvalid
	}
	if _, err := time.Parse("2006-01-02", item.Date); err != nil {
		return nil, ErrInvalid
	}
	if item.Available && !validClockRange(item.StartTime, item.EndTime) {
		return nil, ErrInvalid
	}
	if !item.Available {
		item.StartTime, item.EndTime = "", ""
	}
	item.ID, item.TechnicianID, item.Reason, item.CreatedAt = utils.GenerateID(), technicianID, strings.TrimSpace(item.Reason), time.Now().UTC()
	if err := s.repo.UpsertException(ctx, item); err != nil {
		return nil, err
	}
	return item, nil
}

func (s *Service) DeleteException(ctx context.Context, technicianID, dateOrID string) error {
	return s.repo.DeleteException(ctx, technicianID, strings.TrimSpace(dateOrID))
}

func (s *Service) Slots(ctx context.Context, technicianID string, from, to time.Time, durationMinutes int) ([]*models.BookableSlot, error) {
	if durationMinutes < 15 || durationMinutes > 480 {
		durationMinutes = 60
	}
	if from.IsZero() {
		from = time.Now().UTC()
	}
	if to.IsZero() {
		to = from.AddDate(0, 0, 14)
	}
	if !to.After(from) || to.Sub(from) > 60*24*time.Hour {
		return nil, ErrInvalid
	}
	rules, err := s.repo.Rules(ctx, technicianID)
	if err != nil || len(rules) == 0 {
		return []*models.BookableSlot{}, err
	}
	exceptions, err := s.repo.Exceptions(ctx, technicianID, from, to)
	if err != nil {
		return nil, err
	}
	busy, err := s.repo.BusyTimes(ctx, technicianID, from, to)
	if err != nil {
		return nil, err
	}
	exceptionByDate := make(map[string]*models.AvailabilityException)
	for _, item := range exceptions {
		exceptionByDate[item.Date] = item
	}
	duration := time.Duration(durationMinutes) * time.Minute
	result := make([]*models.BookableSlot, 0)
	seenStarts := make(map[int64]bool)
	for _, rule := range rules {
		zone, _ := time.LoadLocation(rule.Timezone)
		localFrom, localTo := from.In(zone), to.In(zone)
		for day := localDay(localFrom); !day.After(localTo); day = day.AddDate(0, 0, 1) {
			if int(day.Weekday()) != rule.Weekday {
				continue
			}
			startText, endText := rule.StartTime, rule.EndTime
			if exception := exceptionByDate[day.Format("2006-01-02")]; exception != nil {
				if !exception.Available {
					continue
				}
				startText, endText = exception.StartTime, exception.EndTime
			}
			start := atClock(day, startText, zone)
			end := atClock(day, endText, zone)
			for slotStart := start; !slotStart.Add(duration).After(end); slotStart = slotStart.Add(duration) {
				slotEnd := slotStart.Add(duration)
				if seenStarts[slotStart.Unix()] || slotStart.Before(from) || !slotStart.Before(to) || overlapsBusy(slotStart, slotEnd, busy, duration) {
					continue
				}
				seenStarts[slotStart.Unix()] = true
				result = append(result, &models.BookableSlot{StartsAt: slotStart.UTC(), EndsAt: slotEnd.UTC(), Timezone: rule.Timezone, TechnicianID: technicianID})
				if len(result) >= 500 {
					return result, nil
				}
			}
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].StartsAt.Before(result[j].StartsAt) })
	return result, nil
}

func validClockRange(start, end string) bool {
	a, aErr := time.Parse("15:04", start)
	b, bErr := time.Parse("15:04", end)
	return aErr == nil && bErr == nil && a.Before(b)
}

func localDay(value time.Time) time.Time {
	y, m, d := value.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, value.Location())
}

func atClock(day time.Time, clock string, zone *time.Location) time.Time {
	parsed, _ := time.Parse("15:04", clock)
	return time.Date(day.Year(), day.Month(), day.Day(), parsed.Hour(), parsed.Minute(), 0, 0, zone)
}

func overlapsBusy(start, end time.Time, busy []time.Time, duration time.Duration) bool {
	for _, item := range busy {
		busyEnd := item.Add(duration)
		if start.Before(busyEnd) && end.After(item) {
			return true
		}
	}
	return false
}

package main

import (
	"context"
	"log"
	"os"

	"ispilolite/pkg/config"
	"ispilolite/pkg/database"
)

func main(){
	if _,err:=config.Load();err!=nil{log.Fatal(err)}
	path:=os.Getenv("CONFIG_PATH");if path==""{path="config/config.yaml"}
	if err:=database.InitDB(path);err!=nil{log.Fatal(err)}
	defer database.CloseDB();_ = context.Background()
	log.Println("database migrations and schema verification completed")
}

package main

import (
	"fmt"
	"log"
	"monkeyy/internal/data"
)


func main() {
	dbPath := "cmd/tui/db.sqlite"
	err := data.InitDataBase(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer data.CloseDataBase()

	sentence, err := data.GetLongSentence()
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Inserting sentence: ", sentence)
	err = data.InsertSentence(sentence)
	if err != nil {
		log.Fatal(err)
	}
	
	fmt.Println("Sentence inserted successfully")
	


}
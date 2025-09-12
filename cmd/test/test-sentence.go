package main

import (
	"fmt"
	"log"
	"monkeyy/data"
)


func main() {
	// The data package now uses an in-memory store, so no database is needed.

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
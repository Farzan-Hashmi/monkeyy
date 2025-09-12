package data

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/dgraph-io/badger/v4"
)

var (
	db *badger.DB
)

const (
	storePrefix    = "store:"
	sentencePrefix = "sentence:"
)

var LONG_SENTENCE_COST = 60

type LeaderBoardEntry struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	WPM      int    `json:"wpm"`
}

type DBEntry struct {
	DateID    string             `json:"date_id"`
	UserStats []LeaderBoardEntry `json:"user_stats"`
}

type LeaderBoardResponse struct {
	DateID             string             `json:"date_id"`
	LeaderboardEntries []LeaderBoardEntry `json:"leaderboard_entries"`
}

func getCurrentDateID() string {
	location, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		log.Printf("failed to load location: %v", err)
		return time.Now().UTC().Format("2006-01-02")
	}
	return time.Now().In(location).Format("2006-01-02")
}

func InitInMemoryStore() {
	opts := badger.DefaultOptions("badger_db")
	opts.Logger = nil
	
	var err error
	db, err = badger.Open(opts)
	if err != nil {
		log.Fatalf("Failed to open Badger database: %v", err)
	}

	if _, err := GetTodaysSentence(); err != nil {
		log.Printf("Pre-generating today's sentence due to: %v", err)
		sentence, err := GetLongSentence()
		if err != nil {
			log.Printf("failed to get long sentence: %v", err)
			return
		}
		if err := InsertSentence(sentence); err != nil {
			log.Printf("failed to insert sentence: %v", err)
		}
	}
}

func Shutdown() {
	log.Println("Shutting down, closing Badger database...")
	if db != nil {
		if err := db.Close(); err != nil {
			log.Printf("Error closing Badger database: %v", err)
		}
	}
}

func setValue(key string, value interface{}) error {
	jsonData, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal value: %w", err)
	}
	
	return db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte(key), jsonData)
	})
}

func getValue(key string, dest interface{}) error {
	return db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(key))
		if err != nil {
			if err == badger.ErrKeyNotFound {
				return fmt.Errorf("key not found")
			}
			return err
		}
		
		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, dest)
		})
	})
}

func GetUserChallengeStatus(userID string) (bool, error) {
	dateId := getCurrentDateID()
	key := storePrefix + dateId
	
	var todayEntry DBEntry
	err := getValue(key, &todayEntry)
	if err != nil {
		return false, nil
	}

	for _, entry := range todayEntry.UserStats {
		if entry.UserID == userID {
			return true, nil
		}
	}

	return false, nil
}

func GetLeaderBoard() (*LeaderBoardResponse, error) {
	dateId := getCurrentDateID()
	key := storePrefix + dateId
	
	var todayEntry DBEntry
	err := getValue(key, &todayEntry)
	if err != nil {
		return &LeaderBoardResponse{
			DateID:             dateId,
			LeaderboardEntries: []LeaderBoardEntry{},
		}, nil
	}

	leaderBoardEntries := make([]LeaderBoardEntry, len(todayEntry.UserStats))
	copy(leaderBoardEntries, todayEntry.UserStats)

	sort.Slice(leaderBoardEntries, func(i, j int) bool {
		return leaderBoardEntries[i].WPM > leaderBoardEntries[j].WPM
	})

	return &LeaderBoardResponse{
		DateID:             dateId,
		LeaderboardEntries: leaderBoardEntries,
	}, nil
}

func GetTodaysSentence() (string, error) {
	dateId := getCurrentDateID()
	key := sentencePrefix + dateId
	
	var sentence string
	err := getValue(key, &sentence)
	if err != nil {
		return "", fmt.Errorf("no sentence for today")
	}
	return sentence, nil
}

func SubmitSentence(ctx context.Context, userID string, username string, wpm int) error {
	dateId := getCurrentDateID()
	key := storePrefix + dateId
	
	var todayEntry DBEntry
	err := getValue(key, &todayEntry)
	if err != nil {
		todayEntry = DBEntry{
			DateID:    dateId,
			UserStats: []LeaderBoardEntry{},
		}
	}

	for _, entry := range todayEntry.UserStats {
		if entry.UserID == userID {
			return fmt.Errorf("user has already submitted a score today")
		}
	}

	todayEntry.UserStats = append(todayEntry.UserStats, LeaderBoardEntry{
		UserID:   userID,
		Username: username,
		WPM:      wpm,
	})

	return setValue(key, todayEntry)
}

func InsertSentence(sentence string) error {
	dateId := getCurrentDateID()
	sentenceKey := sentencePrefix + dateId
	storeKey := storePrefix + dateId
	
	if err := setValue(sentenceKey, sentence); err != nil {
		return fmt.Errorf("failed to save sentence: %w", err)
	}
	
	var todayEntry DBEntry
	err := getValue(storeKey, &todayEntry)
	if err != nil {
		todayEntry = DBEntry{
			DateID:    dateId,
			UserStats: []LeaderBoardEntry{},
		}
		return setValue(storeKey, todayEntry)
	}

	return nil
}

func GetLongSentence() (string, error) {
	sentences := []string{}
	totalWords := 0

	for totalWords < 38 {
		s, err := getRandomSentence()
		if err != nil {
			return "", fmt.Errorf("failed to get random sentence: %v", err)
		}
		sentences = append(sentences, s)
		allText := strings.Join(sentences, " ")
		totalWords = len(strings.Fields(allText))
	}

	finalSentence := strings.Join(sentences, " ")

	words := strings.Fields(finalSentence)
	if len(words) > 38 {
		finalSentence = strings.Join(words[:38], " ")
	}

	finalSentence = strings.ReplaceAll(finalSentence, "\n", " ")
	// lowercase
	finalSentence = strings.ToLower(finalSentence)
	// replace period with period space
	finalSentence = strings.ReplaceAll(finalSentence, ".", ". ")
	// replace comma with comma space
	finalSentence = strings.ReplaceAll(finalSentence, ",", ", ")
	// replace semicolon with semicolon space
	finalSentence = strings.ReplaceAll(finalSentence, ";", "; ")
	// replace colon with colon space
	finalSentence = strings.ReplaceAll(finalSentence, ":", ": ")
	// replace question mark with question mark space
	finalSentence = strings.ReplaceAll(finalSentence, "?", "? ")
	// replace exclamation mark with exclamation mark space
	finalSentence = strings.ReplaceAll(finalSentence, "!", "! ")
	// replace parentheses with parentheses space
	finalSentence = strings.ReplaceAll(finalSentence, "  ", " ")
	// replace single ’ quotes with single quote space
	finalSentence = strings.ReplaceAll(finalSentence, "’", "'")
	// replace double ” quotes with double quote space
	finalSentence = strings.ReplaceAll(finalSentence, "”", "\"")
	// replace double ‘ quotes with double quote space
	finalSentence = strings.ReplaceAll(finalSentence, "‘", "'")
	// replace double “ quotes with double quote space
	finalSentence = strings.TrimSpace(finalSentence)

	return finalSentence, nil
}

func getRandomSentence() (string, error) {
	url := "http://thequoteshub.com/api/random-quote"
	response, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to get random sentence: %v", err)
	}
	defer response.Body.Close()

	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %v", err)
	}

	var quote map[string]interface{}
	err = json.Unmarshal(body, &quote)
	if err != nil {
		return "", fmt.Errorf("failed to unmarshal response body: %v", err)
	}

	quoteText, exists := quote["text"]
	if !exists {
		return "", fmt.Errorf("failed to get quote text")
	}

	quoteString, ok := quoteText.(string)
	if !ok {
		return "", fmt.Errorf("failed to get quote text")
	}

	return quoteString, nil
}
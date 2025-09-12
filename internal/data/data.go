package data

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)


var db*sql.DB
var LONG_SENTENCE_COST = 60

type LeaderBoardEntry struct {
	UserID string `json:"user_id"`
	Username string `json:"username"`
	WPM int `json:"wpm"`
}

type DBEntry struct {
	DateID string `json:"date_id"`
	UserStats []LeaderBoardEntry `json:"user_stats"`
}

type LeaderBoardResponse struct {
	DateID string `json:"date_id"`
	LeaderboardEntries []LeaderBoardEntry `json:"leaderboard_entries"`
}

func InitDataBase(dbPath string) error {
	var err error
	db, err = sql.Open("sqlite3", dbPath)
	if err != nil {
		return fmt.Errorf("failed to open DB: %v", err)
		return err
	}

	_, err = db.Exec("PRAGMA journal_mode = WAL")
	if err != nil {
		return fmt.Errorf("failed to set journal mode: %v", err)
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS sentences (
		date_id TEXT PRIMARY KEY,
		sentence TEXT NOT NULL,
		value TEXT NOT NULL
	)
	`)
	if err != nil {
		return fmt.Errorf("failed to create sentences table: %v", err)
	}


	return nil
}

func CloseDataBase() error {
	if db != nil {
		return db.Close()
	}
	return nil
}

func GetUserChallengeStatus(userID string) (bool, error) {
	dateId := time.Now().Format("2006-01-02")

	query := `SELECT value FROM sentences where date_id = ?`
	row := db.QueryRow(query, dateId)

	var value string
	err := row.Scan(&value)
	if err != nil {
		return false, fmt.Errorf("failed to get value: %v", err)
	}

	var unmarshalledValue DBEntry
	err = json.Unmarshal([]byte(value), &unmarshalledValue)
	if err != nil {
		return false, fmt.Errorf("failed to unmarshal value: %v", err)
	}

	for _, entry := range unmarshalledValue.UserStats {
		if entry.UserID == userID {
			return true, nil
		}
	}

	return false, nil
}

func GetLeaderBoard() (*LeaderBoardResponse, error){
	dateId := time.Now().Format("2006-01-02")

	query := `SELECT value FROM sentences where date_id = ?`
	row := db.QueryRow(query, dateId)

	var value string
	err := row.Scan(&value)
	if err != nil {
		return nil, fmt.Errorf("failed to get value: %v", err)
	}
	
	var unmarshalledValue DBEntry
	err = json.Unmarshal([]byte(value), &unmarshalledValue)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal value: %v", err)
	}

	leaderBoardEntries := make([]LeaderBoardEntry, len(unmarshalledValue.UserStats))
	copy(leaderBoardEntries, unmarshalledValue.UserStats)

	sort.Slice(leaderBoardEntries, func(i, j int) bool {
		return leaderBoardEntries[i].WPM > leaderBoardEntries[j].WPM
	})

	return &LeaderBoardResponse{
		DateID: dateId,
		LeaderboardEntries: leaderBoardEntries,
	}, nil
	
}

func GetTodaysSentence() (string, error) {
	dateId := time.Now().Format("2006-01-02")

	query := `SELECT sentence FROM sentences where date_id = ?`
	row := db.QueryRow(query, dateId)

	var sentence string
	err := row.Scan(&sentence)
	if err != nil {
		return "", fmt.Errorf("failed to get sentence: %v", err)
	}
	return sentence, nil
}

func SubmitSentence(ctx context.Context, userID string, username string, wpm int) error {
	dateId := time.Now().Format("2006-01-02")

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %v", err)
	}
	defer tx.Rollback()

	query := `SELECT value FROM sentences WHERE date_id = ?`
	row := tx.QueryRow(query, dateId)

	var value string
	err = row.Scan(&value)
	if err != nil {
		return fmt.Errorf("failed to get value: %v", err)
	}

	var unmarshalledValue DBEntry
	err = json.Unmarshal([]byte(value), &unmarshalledValue)

	if err != nil {
		return fmt.Errorf("failed to unmarshal value: %v", err)
	}

	for _, entry := range unmarshalledValue.UserStats {
		if entry.UserID == userID {
			fmt.Printf("User %s already exists in the leaderboard\n", userID)
		}
	}

	unmarshalledValue.UserStats = append(unmarshalledValue.UserStats, LeaderBoardEntry{
		UserID: userID,
		Username: username,
		WPM: wpm,
	})

	marshalledValue, err := json.Marshal(unmarshalledValue)
	if err != nil {
		return fmt.Errorf("failed to marshal value: %v", err)
	}

	_, err = tx.Exec(`UPDATE sentences SET value = ? WHERE date_id = ?`, marshalledValue, dateId)
	if err != nil {
		return fmt.Errorf("failed to update value: %v", err)
	}

	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("failed to commit transaction: %v", err)
	}

	return nil
	
}

func InsertSentence(sentence string) error {
	dateId := time.Now().Format("2006-01-02")
	log.Printf("Inserting sentence %s for date %s", sentence, dateId)
	emptyDBEntry := DBEntry{
		DateID: dateId,
		UserStats: []LeaderBoardEntry{},
	}

	jsonEntry, err := json.Marshal(emptyDBEntry)
	if err != nil {
		return fmt.Errorf("failed to marshal value: %v", err)
	}

	jsonEntryString := string(jsonEntry)

	_, err = db.ExecContext(
		context.Background(),
		`INSERT INTO sentences (date_id, sentence, value) VALUES (?, ?, ?)`,
		dateId,
		sentence,
		jsonEntryString,
	)
	if err != nil {
		return fmt.Errorf("failed to insert sentence: %v", err)
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
	if len(words) > 1 {
		finalSentence = strings.Join(words[:1], " ")
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
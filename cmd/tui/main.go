package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/charmbracelet/wish/bubbletea"
	"github.com/charmbracelet/wish/logging"
	"github.com/google/uuid"
	"github.com/robfig/cron/v3"

	"monkeyy/internal/data"
)


const (
   host = "0.0.0.0" // Bind to all interfaces for production
   port = "2222"
)


func main() {
   // Initialize database
   fmt.Println("Initializing database...")
   dbPath := "db.sqlite"
   err := data.InitDataBase(dbPath)
   if err != nil {
       log.Error("Error initializing database", "error", err)
       return
   }
   defer data.CloseDataBase()


   // Initialize cron scheduler for daily sentence generation
   c := initCronScheduler()
   c.Start()
   defer c.Stop()


   hostKeyPath := os.Getenv("SSH_HOST_KEY_PATH")
   if hostKeyPath == "" {
       hostKeyPath = ".ssh/id_ed25519"
   }


   s, err := wish.NewServer(
       wish.WithAddress(net.JoinHostPort(host, port)),
       wish.WithHostKeyPath(hostKeyPath),
       wish.WithPublicKeyAuth(func(ctx ssh.Context, key ssh.PublicKey) bool {
           return true // Accept any public key
       }),
       wish.WithPasswordAuth(func(ctx ssh.Context, password string) bool {
           return false // Reject all passwords
       }),
       wish.WithMiddleware(
           bubbletea.Middleware(teaHandler),
           // activeterm.Middleware(), // Bubble Tea apps usually require a PTY.
           logging.Middleware(),
       ),
   )
   if err != nil {
       log.Error("Could not start server", "error", err)
   }


   done := make(chan os.Signal, 1)
   signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
   log.Info("Starting SSH server", "host", host, "port", port)
   go func() {
       if err = s.ListenAndServe(); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
           log.Error("Could not start server", "error", err)
           done <- nil
       }
   }()


   <-done
   log.Info("Stopping SSH server")
   ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
   defer func() { cancel() }()
   if err := s.Shutdown(ctx); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
       log.Error("Could not stop server", "error", err)
   }
}


// initCronScheduler sets up the daily sentence generation cron job
func initCronScheduler() *cron.Cron {
   c := cron.New()


   // Run every 24 hours at midnight
   c.AddFunc("0 0 * * *", func() {
       fmt.Println("Cron job started")
       sentence, err := data.GetLongSentence()
       if err != nil {
           log.Error("Error getting long sentence", "error", err)
           return
       }
       err = data.InsertSentence(sentence)
       if err != nil {
           log.Error("Error inserting sentence", "error", err)
           return
       }


       log.Info("Daily sentence generated successfully", "sentence", sentence)
   })


   return c
}


// You can wire any Bubble Tea model up to the middleware with a function that
// handles the incoming ssh.Session. Here we just grab the terminal info and
// pass it to the new model. You can also return tea.ProgramOptions (such as
// tea.WithAltScreen) on a session by session basis.
func teaHandler(s ssh.Session) (tea.Model, []tea.ProgramOption) {
   // This should never fail, as we are using the activeterm middleware.
   // pty, _, _ := s.Pty()


   // When running a Bubble Tea app over SSH, you shouldn't use the default
   // lipgloss.NewStyle function.
   // That function will use the color profile from the os.Stdin, which is the
   // server, not the client.
   // We provide a MakeRenderer function in the bubbletea middleware package,
   // so you can easily get the correct renderer for the current session, and
   // use it to create the styles.
   // The recommended way to use these styles is to then pass them down to
   // your Bubble Tea model.
   renderer := bubbletea.MakeRenderer(s)


   // Create styles using the session renderer for proper color profile handling
   correctStyle := renderer.NewStyle().Foreground(lipgloss.Color("#10b981"))                                         // Green (typed correctly)
   incorrectStyle := renderer.NewStyle().Foreground(lipgloss.Color("#ef4444")).Background(lipgloss.Color("#7f1d1d")) // Red with bg (typed incorrectly)
   normalStyle := renderer.NewStyle().Foreground(lipgloss.Color("#6b7280"))                                          // Gray (not typed yet)
   currentStyle := renderer.NewStyle().Foreground(lipgloss.Color("#ffffff")).Background(lipgloss.Color("#3b82f6"))   // Blue bg (current char)
   statsStyle := renderer.NewStyle().Foreground(lipgloss.Color("#8b5cf6")).Bold(true)                                // Purple stats


   var userIdentifier string
   if pubKey := s.PublicKey(); pubKey != nil {
       hash := sha256.Sum256(pubKey.Marshal())
       userIdentifier = fmt.Sprintf("%x", hash)
       // Using public key auth
   }


   userIdentifier += randomIdGenerator()


   // User public key hashed and ready


   m := NewModelWithStyles(correctStyle, incorrectStyle, normalStyle, currentStyle, statsStyle, userIdentifier)


   return m, []tea.ProgramOption{tea.WithAltScreen()}
}


func randomIdGenerator() string {
   return uuid.New().String()
}


var MOCK_USER_ID = randomIdGenerator()


type leaderboardEntry struct {
   UserID   string `json:"UserID"`
   Username string `json:"Username"`
   WPM      int    `json:"WPM"`
}


type userDailyChallengeStatusReceivedMsg struct {
   userAlreadyDidDailyChallenge bool
}


type leaderboardReceivedMsg struct {
   DateID             string             `json:"DateID"`
   LeaderboardEntries []leaderboardEntry `json:"LeaderboardEntries"`
}


type randomSentenceReceivedMsg struct {
   sentence string
}


type sentenceSubmittedMsg struct {
   success bool
   message string
}


func fetchUserDailyChallengeStatusCmd(userId string) tea.Cmd {
   return func() tea.Msg {
       userAlreadyDidDailyChallenge, err := data.GetUserChallengeStatus(userId)
       if err != nil {
           log.Error("Error fetching user daily challenge status", "error", err)
           return userDailyChallengeStatusReceivedMsg{userAlreadyDidDailyChallenge: false}
       }


       return userDailyChallengeStatusReceivedMsg{userAlreadyDidDailyChallenge: userAlreadyDidDailyChallenge}
   }
}


func fetchTodaysLeaderBoardCmd() tea.Cmd {
   return func() tea.Msg {
       leaderboard, err := data.GetLeaderBoard()
       if err != nil {
           log.Error("Error fetching leaderboard", "error", err)
           return leaderboardReceivedMsg{DateID: "", LeaderboardEntries: []leaderboardEntry{}}
       }


       // Convert data.LeaderboardEntry to local leaderboardEntry type
       entries := make([]leaderboardEntry, len(leaderboard.LeaderboardEntries))
       for i, entry := range leaderboard.LeaderboardEntries {
           entries[i] = leaderboardEntry{
               UserID:   entry.UserID,
               Username: entry.Username,
               WPM:      entry.WPM,
           }
       }


       return leaderboardReceivedMsg{
           DateID:             leaderboard.DateID,
           LeaderboardEntries: entries,
       }
   }
}


func getRandomSentenceCmd() tea.Cmd {
   return func() tea.Msg {
       sentence, err := data.GetTodaysSentence()
       if err != nil {
           log.Error("Error fetching random sentence", "error", err)
           return randomSentenceReceivedMsg{sentence: ""}
       }


       return randomSentenceReceivedMsg{sentence: sentence}
   }
}


func submitSentenceCmd(userId string, username string, wpm int) tea.Cmd {
   return func() tea.Msg {
       err := data.SubmitSentence(context.Background(), userId, username, wpm)
       if err != nil {
           log.Error("Error submitting sentence", "error", err)
           return sentenceSubmittedMsg{success: false, message: err.Error()}
       }


       return sentenceSubmittedMsg{success: true, message: "Sentence submitted successfully"}
   }
}


type tickMsg struct{}


func tickCmd() tea.Cmd {
   return tea.Tick(time.Millisecond*200, func(t time.Time) tea.Msg {
       return tickMsg{}
   })
}


type leaderboardPollMsg struct{}


func leaderboardPollCmd() tea.Cmd {
   return tea.Tick(time.Second*30, func(t time.Time) tea.Msg {
       return leaderboardPollMsg{}
   })
}


func (m model) Init() tea.Cmd {
   return tea.Batch(
       fetchUserDailyChallengeStatusCmd(m.userPublicKey),
       getRandomSentenceCmd(),
       tickCmd(), // Start the tick timer
   )


}


type model struct {
   hasUserAlreadyDoneDailyChallenge bool
   userSetUsername                  bool
   username                         string
   usernameInput                    textinput.Model


   // leaderboard related fields
   dateID             string
   LeaderboardEntries []leaderboardEntry


   // typing test related fields
   textToType         string
   textUserTyped      string
   WPM                int
   startTime          time.Time
   didUserStartTyping bool


   // viewport size
   width  int
   height int


   correctStyle   lipgloss.Style
   incorrectStyle lipgloss.Style
   normalStyle    lipgloss.Style
   currentStyle   lipgloss.Style
   statsStyle     lipgloss.Style
   userPublicKey  string
}


func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
   switch msg := msg.(type) {


   case tea.WindowSizeMsg:
       m.width = msg.Width
       m.height = msg.Height
       return m, nil


   case randomSentenceReceivedMsg:
       m.textToType = msg.sentence
       return m, nil


   case sentenceSubmittedMsg:
       if msg.success {
           return m, fetchTodaysLeaderBoardCmd()
       } else {
           //fmt.Println("Failed to submit sentence:", msg.message)
       }
       return m, nil


   // leaderboard related updates
   case userDailyChallengeStatusReceivedMsg:
       m.hasUserAlreadyDoneDailyChallenge = msg.userAlreadyDidDailyChallenge
       return m, fetchTodaysLeaderBoardCmd()


   case leaderboardReceivedMsg:
       m.dateID = msg.DateID
       m.LeaderboardEntries = msg.LeaderboardEntries
       // Debug output removed to reduce server load
       // start polling for leaderboard updates if we're on the leaderboard screen
       if m.hasUserAlreadyDoneDailyChallenge {
           return m, leaderboardPollCmd()
       }
       return m, nil


   case leaderboardPollMsg:
       // continue polling if we're still on the leaderboard screen
       if m.hasUserAlreadyDoneDailyChallenge {
           return m, tea.Batch(fetchTodaysLeaderBoardCmd(), leaderboardPollCmd())
       }
       return m, nil


   // typing test related updates


   case tickMsg:
       if m.didUserStartTyping {
           totalCorrectCharactersTyped := 0
           for i, char := range m.textUserTyped {
               if i < len(m.textToType) && char == []rune(m.textToType)[i] {
                   totalCorrectCharactersTyped++
               }
           }
           m.WPM = int(float64(totalCorrectCharactersTyped) / 5.0 / time.Since(m.startTime).Minutes())
           if didUserFinishTyping(m) && !m.hasUserAlreadyDoneDailyChallenge {
               // User finished typing, submitting sentence
               m.hasUserAlreadyDoneDailyChallenge = true
               return m, submitSentenceCmd(m.userPublicKey, m.username, m.WPM)
           }
       }
       if !m.hasUserAlreadyDoneDailyChallenge {
           return m, tickCmd()
       }
       return m, nil
   case tea.KeyMsg:
       if msg.String() == "ctrl+c" {
           return m, tea.Quit
       }


       if !m.userSetUsername {
           if msg.String() == "enter" {
               username := strings.TrimSpace(m.usernameInput.Value())
               if len(username) >= 6 {
                   m.username = username
                   m.userSetUsername = true
                   m.usernameInput.Blur()
               }
               return m, nil
           }
           var cmd tea.Cmd
           m.usernameInput, cmd = m.usernameInput.Update(msg)
           return m, cmd
       }


       if msg.String() == "backspace" {
           m.didUserStartTyping = true
           if len(m.textUserTyped) == 0 {
               return m, nil
           }
           if len(m.textUserTyped) > 0 {
               m.textUserTyped = m.textUserTyped[:len(m.textUserTyped)-1]
               // Check if string is not empty before accessing the last character
               if len(m.textUserTyped) > 0 && m.textUserTyped[len(m.textUserTyped)-1] == '\n' {
                   m.textUserTyped = m.textUserTyped[:len(m.textUserTyped)-1]
               }
           }
           return m, nil
       }


       if len(msg.String()) == 1 {
           m.didUserStartTyping = true
           r := rune(msg.String()[0])
           if unicode.IsLetter(r) || unicode.IsPunct(r) || unicode.IsSpace(r) || unicode.IsNumber(r) {
               if len(m.textUserTyped) == 0 {
                   m.startTime = time.Now()
               }
               if len(m.textUserTyped) < len(m.textToType) {
                   nextChar := []rune(m.textToType)[len([]rune(m.textUserTyped))]
                   if nextChar == '\n' {
                       m.textUserTyped += "\n"
                       if len(m.textUserTyped) < len(m.textToType) {
                           m.textUserTyped += msg.String()
                       }
                   } else {
                       m.textUserTyped += msg.String()
                   }
               }
           }


           return m, nil
       }


   }


   return m, nil
}


func (m model) View() string {
   if m.hasUserAlreadyDoneDailyChallenge {
       // TODO: show leaderboard
       return renderLeaderboard(m)
   } else {
       if m.userSetUsername {
           return renderTypingTest(m)
       } else {
           return renderUsernamePrompt(m)
       }


   }


}


func createUsernameInput() textinput.Model {
   ti := textinput.New()
   ti.Placeholder = "Enter your username..."
   ti.Focus()
   ti.CharLimit = 20
   ti.Width = 50 // Wider to match other content
   return ti
}


func NewModel() model {
   // Default styles for non-SSH usage (fallback)
   return model{
       textToType:     "Loading sentence...",
       WPM:            0,
       startTime:      time.Now(),
       usernameInput:  createUsernameInput(),
       correctStyle:   lipgloss.NewStyle().Foreground(lipgloss.Color("#10b981")),
       incorrectStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("#ef4444")).Background(lipgloss.Color("#7f1d1d")),
       normalStyle:    lipgloss.NewStyle().Foreground(lipgloss.Color("#6b7280")),
       currentStyle:   lipgloss.NewStyle().Foreground(lipgloss.Color("#ffffff")).Background(lipgloss.Color("#3b82f6")),
       statsStyle:     lipgloss.NewStyle().Foreground(lipgloss.Color("#8b5cf6")).Bold(true),
   }
}


func NewModelWithStyles(correctStyle, incorrectStyle, normalStyle, currentStyle, statsStyle lipgloss.Style, userPublicKey string) model {
   return model{
       textToType:     "Loading sentence...",
       WPM:            0,
       startTime:      time.Now(),
       usernameInput:  createUsernameInput(),
       correctStyle:   correctStyle,
       incorrectStyle: incorrectStyle,
       normalStyle:    normalStyle,
       currentStyle:   currentStyle,
       statsStyle:     statsStyle,
       userPublicKey:  userPublicKey,
   }
}


// helper methods


func didUserFinishTyping(m model) bool {
   return len(m.textUserTyped) == len([]rune(m.textToType)) && m.textUserTyped == m.textToType
}


func renderLeaderboard(m model) string {
   // add date id as title first
   titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#00d4aa")).Bold(true)
   dateIDTitle := titleStyle.Render("🏆 Daily Leaderboard - " + m.dateID)
   leaderboardDisplay := []string{dateIDTitle, ""}


   if len(m.LeaderboardEntries) == 0 {
       emptyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).Italic(true)
       leaderboardDisplay = append(leaderboardDisplay, emptyStyle.Render("   No entries yet today!"))
   } else {
       for i, entry := range m.LeaderboardEntries {
           username := entry.Username


           // Add medals for top 3
           var prefix string
           var entryStyle lipgloss.Style
           switch i {
           case 0:
               prefix = "🥇"
               entryStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffd700")).Bold(true) // Gold
           case 1:
               prefix = "🥈"
               entryStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#c0c0c0")).Bold(true) // Silver
           case 2:
               prefix = "🥉"
               entryStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#cd7f32")).Bold(true) // Bronze
           default:
               prefix = fmt.Sprintf("%2d.", i+1)
               entryStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffffff"))
           }


           entryText := fmt.Sprintf(" %s %s: %d WPM", prefix, username, entry.WPM)
           leaderboardDisplay = append(leaderboardDisplay, entryStyle.Render(entryText))
       }
   }


   return lipgloss.JoinVertical(lipgloss.Left, leaderboardDisplay...)
}


func renderTypingTest(m model) string {
   typedText := m.textUserTyped
   needToTypeTextRunes := []rune(m.textToType)


   var textBuilder strings.Builder


   typedLength := len([]rune(typedText))
   foundError := false
   highlightNextAsCurrent := false


   // Process each character in the text to type
   for i, char := range needToTypeTextRunes {
       if char == '\n' {
           if i == typedLength {
               highlightNextAsCurrent = true
           }
           textBuilder.WriteRune('\n')
           continue
       }


       if highlightNextAsCurrent {
           textBuilder.WriteString(m.currentStyle.Render(string(char)))
           highlightNextAsCurrent = false
           continue
       }


       if i < typedLength {
           typedChar := []rune(typedText)[i]
           if foundError {
               if i >= 0 && i < len(needToTypeTextRunes) {
                   textBuilder.WriteString(m.incorrectStyle.Render(string(needToTypeTextRunes[i])))
               }
           } else if typedChar != char {
               foundError = true
               if i >= 0 && i < len(needToTypeTextRunes) {
                   textBuilder.WriteString(m.incorrectStyle.Render(string(needToTypeTextRunes[i])))
               }
           } else {
               textBuilder.WriteString(m.correctStyle.Render(string(typedChar)))
           }
       } else if i == typedLength {
           textBuilder.WriteString(m.currentStyle.Render(string(char)))
       } else {
           textBuilder.WriteString(m.normalStyle.Render(string(char)))
       }
   }


   textDisplay := textBuilder.String()
   wpmDisplay := m.statsStyle.Render(fmt.Sprintf("WPM: %d", m.WPM))


   if m.width > 0 {
       center := lipgloss.NewStyle().Width(m.width).Align(lipgloss.Left)
       textDisplay = center.Render(textDisplay)
       wpmDisplay = center.Render(wpmDisplay)
   }


   return lipgloss.JoinVertical(lipgloss.Left,
       "",
       textDisplay,
       "",
       "",
       wpmDisplay,
   )
}


func renderUsernamePrompt(m model) string {
   titleStyle := lipgloss.NewStyle().
       Foreground(lipgloss.Color("#00d4aa")).
       Bold(true).
       MarginBottom(2)


   instructionStyle := lipgloss.NewStyle().
       Foreground(lipgloss.Color("#888888")).
       MarginBottom(1)


   title := titleStyle.Render("Daily TUI Typing Challenge")
   instruction := instructionStyle.Render("Please enter today's username to continue: (min 6 characters)")
   inputPrompt := instructionStyle.Render("Press Enter to confirm")


   inputStyle := lipgloss.NewStyle().Align(lipgloss.Center)
   centeredInput := inputStyle.Render(m.usernameInput.View())


   // Center everything if we have width
   if m.width > 0 {
       center := lipgloss.NewStyle().Width(m.width).Align(lipgloss.Center)
       return center.Render(lipgloss.JoinVertical(lipgloss.Center,
           "",
           "",
           title,
           instruction,
           "",
           centeredInput,
           "",
           inputPrompt,
       ))
   }


   return lipgloss.JoinVertical(lipgloss.Center,
       "",
       "",
       title,
       instruction,
       "",
       centeredInput,
       "",
       inputPrompt,
   )
}





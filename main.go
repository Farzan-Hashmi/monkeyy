package main

import (
	"context"
	"errors"
	"fmt"
	"monkeyy/data"
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
	"github.com/charmbracelet/wish/activeterm"
	"github.com/charmbracelet/wish/bubbletea"
	"github.com/charmbracelet/wish/logging"
	recovermw "github.com/charmbracelet/wish/recover"
	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
)


const (
   host = "0.0.0.0" // Bind to all interfaces for production
   port = "22"
)


func main() {
   // Initialize database
   fmt.Println("Initializing database...")
   data.InitInMemoryStore()


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
       wish.WithMiddleware(
        recovermw.Middleware(
            activeterm.Middleware(),
            bubbletea.Middleware(teaHandler),
            logging.Middleware(),
        ),
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
   data.Shutdown() // Save data before shutting down
   log.Info("Stopping SSH server")
   ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
   defer func() { cancel() }()
   if err := s.Shutdown(ctx); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
       log.Error("Could not stop server", "error", err)
   }
}


// initCronScheduler sets up the daily sentence generation cron job
func initCronScheduler() *cron.Cron {
	location, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		log.Fatal("Could not load location for cron", "error", err)
	}

	c := cron.New(cron.WithLocation(location))

	c.AddFunc("0 0 * * *", func() {
		defer func() {
			if r := recover(); r != nil {
				log.Error("Panic in cron job", "panic", r)
			}
		}()

		log.Info("Cron job started - generating daily sentence")
		sentence, err := data.GetLongSentence()
		if err != nil {
			log.Error("Error getting long sentence", "error", err)
			return
		}

		log.Debug("Generated sentence", "length", len(sentence))
		err = data.InsertSentence(sentence)
		if err != nil {
			log.Error("Error inserting sentence", "error", err)
			return
		}

		log.Info("Daily sentence generated successfully", "sentence_length", len(sentence))
	})


	return c
}


// You can wire any Bubble Tea model up to the middleware with a function that
// handles the incoming ssh.Session. Here we just grab the terminal info and
// pass it to the new model. You can also return tea.ProgramOptions (such as
// tea.WithAltScreen) on a session by session basis.
func teaHandler(s ssh.Session) (tea.Model, []tea.ProgramOption) {
   log.Debug("New SSH session started", "remote_addr", s.RemoteAddr().String())

   // This should never fail, as we are using the activeterm middleware.
   // pty, _, _ := s.Pty()

   defer func() {
       if r := recover(); r != nil {
           log.Error("Panic in teaHandler", "panic", r, "remote_addr", s.RemoteAddr().String())
       }
   }()

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
   log.Debug("Renderer created successfully")


   // Create styles using the session renderer for proper color profile handling
   correctStyle := renderer.NewStyle().Foreground(lipgloss.Color("#10b981"))                                         // Green (typed correctly)
   incorrectStyle := renderer.NewStyle().Foreground(lipgloss.Color("#ef4444")).Background(lipgloss.Color("#7f1d1d")) // Red with bg (typed incorrectly)
   normalStyle := renderer.NewStyle().Foreground(lipgloss.Color("#6b7280"))                                          // Gray (not typed yet)
   currentStyle := renderer.NewStyle().Foreground(lipgloss.Color("#ffffff")).Background(lipgloss.Color("#3b82f6"))   // Blue bg (current char)
   statsStyle := renderer.NewStyle().Foreground(lipgloss.Color("#8b5cf6")).Bold(true)                                // Purple stats


   var userIdentifier string
//    if pubKey := s.PublicKey(); pubKey != nil {
//        log.Debug("Public key auth detected")
//        hash := sha256.Sum256(pubKey.Marshal())
//        userIdentifier = fmt.Sprintf("%x", hash)
//        userIdDisplay := userIdentifier
//        if len(userIdentifier) > 16 {
//            userIdDisplay = userIdentifier[:16] + "..."
//        }
//        log.Debug("User identifier generated", "user_id", userIdDisplay)
//        // Using public key auth
//    } else {
       log.Debug("No public key found, using username and IP as identifier")
       remoteAddr := s.RemoteAddr().String()
       ip, _, err := net.SplitHostPort(remoteAddr)
       if err != nil {
           // If parsing fails, use the whole remote address string as a fallback for the ip part
            log.Warn("Could not parse IP from remote address", "remote_addr", remoteAddr, "error", err)
            ip = remoteAddr
       }
       user := s.User()
       if user == "" {
           user = "anonymous"
       }
       userIdentifier = fmt.Sprintf("%s-%s", user, ip)
       log.Debug("User identifier generated", "user_id", userIdentifier)



//    userIdentifier += randomIdGenerator()

   // User public key hashed and ready
   log.Debug("Creating new model with styles")

   m := NewModelWithStyles(correctStyle, incorrectStyle, normalStyle, currentStyle, statsStyle, userIdentifier)
   log.Debug("Model created successfully")

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
       defer func() {
           if r := recover(); r != nil {
               log.Error("Panic in fetchUserDailyChallengeStatusCmd", "panic", r, "user_id", userId)
           }
       }()

       userIdDisplay := userId
       if len(userId) > 16 {
           userIdDisplay = userId[:16] + "..."
       }

       log.Debug("Fetching user daily challenge status", "user_id", userIdDisplay)
       userAlreadyDidDailyChallenge, err := data.GetUserChallengeStatus(userId)
       if err != nil {
           log.Error("Error fetching user daily challenge status", "error", err, "user_id", userIdDisplay)
           return userDailyChallengeStatusReceivedMsg{userAlreadyDidDailyChallenge: false}
       }

       log.Debug("User daily challenge status fetched", "already_done", userAlreadyDidDailyChallenge, "user_id", userIdDisplay)
       return userDailyChallengeStatusReceivedMsg{userAlreadyDidDailyChallenge: userAlreadyDidDailyChallenge}
   }
}


func fetchTodaysLeaderBoardCmd() tea.Cmd {
   return func() tea.Msg {
       defer func() {
           if r := recover(); r != nil {
               log.Error("Panic in fetchTodaysLeaderBoardCmd", "panic", r)
           }
       }()

       log.Debug("Fetching today's leaderboard")
       leaderboard, err := data.GetLeaderBoard()
       if err != nil {
           log.Error("Error fetching leaderboard", "error", err)
           return leaderboardReceivedMsg{DateID: "", LeaderboardEntries: []leaderboardEntry{}}
       }

       log.Debug("Leaderboard fetched", "date_id", leaderboard.DateID, "entries_count", len(leaderboard.LeaderboardEntries))

       // Convert LeaderboardEntry to local leaderboardEntry type
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
       defer func() {
           if r := recover(); r != nil {
               log.Error("Panic in getRandomSentenceCmd", "panic", r)
           }
       }()

       log.Debug("Fetching today's sentence")
       sentence, err := data.GetTodaysSentence()
       if err != nil {
           log.Error("Error fetching random sentence", "error", err)
           return randomSentenceReceivedMsg{sentence: ""}
       }

       log.Debug("Sentence fetched", "length", len(sentence))
       return randomSentenceReceivedMsg{sentence: sentence}
   }
}


func submitSentenceCmd(userId string, username string, wpm int) tea.Cmd {
   return func() tea.Msg {
       defer func() {
           if r := recover(); r != nil {
               log.Error("Panic in submitSentenceCmd", "panic", r, "user_id", userId, "username", username, "wpm", wpm)
           }
       }()

       userIdDisplay := userId
       if len(userId) > 16 {
           userIdDisplay = userId[:16] + "..."
       }

       log.Debug("Submitting sentence", "user_id", userIdDisplay, "username", username, "wpm", wpm)
       err := data.SubmitSentence(context.Background(), userId, username, wpm)
       if err != nil {
           log.Error("Error submitting sentence", "error", err, "user_id", userIdDisplay, "username", username, "wpm", wpm)
           return sentenceSubmittedMsg{success: false, message: err.Error()}
       }

       log.Info("Sentence submitted successfully", "user_id", userIdDisplay, "username", username, "wpm", wpm)
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
   return tea.Tick(time.Second*1, func(t time.Time) tea.Msg {
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
	currentPage        int
	entriesPerPage     int
	countdown          string


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
   defer func() {
       if r := recover(); r != nil {
           userIdDisplay := m.userPublicKey
           if len(m.userPublicKey) > 16 {
               userIdDisplay = m.userPublicKey[:16] + "..."
           }
           log.Error("Panic in model Update", "panic", r, "msg_type", fmt.Sprintf("%T", msg), "user_id", userIdDisplay)
       }
   }()

   switch msg := msg.(type) {

   case tea.WindowSizeMsg:
       log.Debug("Window size update", "width", msg.Width, "height", msg.Height)
       m.width = msg.Width
       m.height = msg.Height
       return m, nil


   case randomSentenceReceivedMsg:
       log.Debug("Random sentence received", "length", len(msg.sentence))
       m.textToType = msg.sentence
       return m, nil

   case sentenceSubmittedMsg:
       log.Debug("Sentence submission result", "success", msg.success, "message", msg.message)
       if msg.success {
           return m, fetchTodaysLeaderBoardCmd()
       } else {
           log.Warn("Sentence submission failed", "message", msg.message)
       }
       return m, nil


   // leaderboard related updates
   case userDailyChallengeStatusReceivedMsg:
       log.Debug("User daily challenge status received", "already_done", msg.userAlreadyDidDailyChallenge)
       m.hasUserAlreadyDoneDailyChallenge = msg.userAlreadyDidDailyChallenge
       return m, fetchTodaysLeaderBoardCmd()

   case leaderboardReceivedMsg:
       log.Debug("Leaderboard received", "date_id", msg.DateID, "entries_count", len(msg.LeaderboardEntries))
       m.dateID = msg.DateID
       m.LeaderboardEntries = msg.LeaderboardEntries
       // start polling for leaderboard updates if we're on the leaderboard screen
       if m.hasUserAlreadyDoneDailyChallenge {
           return m, leaderboardPollCmd()
       }
       return m, nil


   case leaderboardPollMsg:
       duration := timeUntilNextMidnight()
       m.countdown = formatDuration(duration)
       // continue polling if we're still on the leaderboard screen
       if m.hasUserAlreadyDoneDailyChallenge {
           return m, fetchTodaysLeaderBoardCmd()
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
           // check for division by zero
           if time.Since(m.startTime).Minutes() > 0 {
               m.WPM = int(float64(totalCorrectCharactersTyped) / 5.0 / time.Since(m.startTime).Minutes())
           }

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

      if m.hasUserAlreadyDoneDailyChallenge {
          totalPages := (len(m.LeaderboardEntries) + m.entriesPerPage - 1) / m.entriesPerPage
          if totalPages == 0 {
              totalPages = 1
          }

          switch msg.String() {
          case "left", "h":
              if m.currentPage > 0 {
                  m.currentPage--
              }
              return m, nil
          case "right", "l":
              if m.currentPage < totalPages-1 {
                  m.currentPage++
              }
              return m, nil
          case "home", "g":
              m.currentPage = 0
              return m, nil
          case "end", "G":
              m.currentPage = totalPages - 1
              return m, nil
          }
      }

      if !m.userSetUsername {
          if msg.String() == "enter" {
              username := strings.TrimSpace(m.usernameInput.Value())
              if len(username) >= 6 && len(username) <= 20 {
                  // Basic validation: only letters, numbers, underscore, and dash
                  valid := true
                  for _, char := range username {
                      if !unicode.IsLetter(char) && !unicode.IsNumber(char) && char != '_' && char != '-' {
                          valid = false
                          break
                      }
                  }
                  if valid {
                      m.username = username
                      m.userSetUsername = true
                      m.usernameInput.Blur()
                  }
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
		currentPage:    0,
		entriesPerPage: 10,
		countdown:      "00:00:00",
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
	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#ffffff")).Bold(true)
	dateIDTitle := titleStyle.Render("🏆 Daily Leaderboard - " + m.dateID)
	leaderboardDisplay := []string{dateIDTitle, ""}

	availableHeight := m.height - 2
	if availableHeight < 5 {
		availableHeight = 5
	}

	if len(m.LeaderboardEntries) == 0 {
		emptyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).Italic(true)
		leaderboardDisplay = append(leaderboardDisplay, emptyStyle.Render("   No entries yet today!"))
	} else {
		totalPages := (len(m.LeaderboardEntries) + m.entriesPerPage - 1) / m.entriesPerPage
		if totalPages == 0 {
			totalPages = 1
		}

		if m.currentPage >= totalPages {
			m.currentPage = totalPages - 1
		}
		if m.currentPage < 0 {
			m.currentPage = 0
		}

		startIdx := m.currentPage * m.entriesPerPage
		endIdx := startIdx + m.entriesPerPage
		if endIdx > len(m.LeaderboardEntries) {
			endIdx = len(m.LeaderboardEntries)
		}
		for i, entry := range m.LeaderboardEntries[startIdx:endIdx] {
			actualIndex := startIdx + i
			username := entry.Username

			var prefix string
			var entryStyle lipgloss.Style
			switch actualIndex {
			case 0:
				prefix = "🥇"
				entryStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffd700")).Bold(true)
			case 1:
				prefix = "🥈"
				entryStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#c0c0c0")).Bold(true)
			case 2:
				prefix = "🥉"
				entryStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#cd7f32")).Bold(true)
			default:
				prefix = fmt.Sprintf("%2d.", actualIndex+1)
				entryStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffffff"))
			}

			entryText := fmt.Sprintf(" %s %s: %d WPM", prefix, username, entry.WPM)
			leaderboardDisplay = append(leaderboardDisplay, entryStyle.Render(entryText))
		}
	}

	paginationStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#666666"))
	controlsStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))

	totalPages := (len(m.LeaderboardEntries) + m.entriesPerPage - 1) / m.entriesPerPage
	if totalPages == 0 {
		totalPages = 1
	}

	pageInfo := fmt.Sprintf("Page %d of %d (%d total entries)", m.currentPage+1, totalPages, len(m.LeaderboardEntries))
	controls := "← → or h l: navigate pages | g: first page | G: last page"
	countdown := fmt.Sprintf("Next challenge in %s", m.countdown)

	spacerWidth := m.width - lipgloss.Width(paginationStyle.Render(pageInfo)) - lipgloss.Width(paginationStyle.Render(countdown))
	if spacerWidth < 0 {
		spacerWidth = 0
	}
	spacer := lipgloss.NewStyle().Width(spacerWidth).Render("")

	bottomLine := lipgloss.JoinHorizontal(lipgloss.Left,
		paginationStyle.Render(pageInfo),
		spacer,
		paginationStyle.Render(countdown),
	)

	contentLines := len(leaderboardDisplay)
	emptyLinesNeeded := availableHeight - contentLines - 3

	if emptyLinesNeeded > 0 {
		for i := 0; i < emptyLinesNeeded; i++ {
			leaderboardDisplay = append(leaderboardDisplay, "")
		}
	}

	leaderboardDisplay = append(leaderboardDisplay, "")
	leaderboardDisplay = append(leaderboardDisplay, bottomLine)
	leaderboardDisplay = append(leaderboardDisplay, controlsStyle.Render(controls))

	return lipgloss.JoinVertical(lipgloss.Left, leaderboardDisplay...)
}

func timeUntilNextMidnight() time.Duration {
	location, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		// Fallback to UTC on error
		now := time.Now().UTC()
		tomorrow := now.Add(24 * time.Hour)
		midnight := time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 0, 0, 0, 0, time.UTC)
		return midnight.Sub(now)
	}
	now := time.Now().In(location)
	tomorrow := now.Add(24 * time.Hour)
	midnight := time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 0, 0, 0, 0, location)
	return midnight.Sub(now)
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
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
		Foreground(lipgloss.Color("#ffffff")).
		Bold(true).
		MarginBottom(1)

	instructionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#888888")).
		MarginBottom(1)

	rulesStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#ffffff")).
		MarginBottom(1)

	highlightStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#ffffff")).
		Bold(true)

	title := titleStyle.Render("🏆 Daily TUI Typing Challenge")
	instruction := instructionStyle.Render("Enter your username to start today's challenge:")

	rules := []string{
		"📋 Rules:",
		"• Username: 6-20 characters (letters, numbers, _ and - only)",
		"• Type the sentence with 100% accuracy and as fast as possible",
		"• You can only play once per day",
		"• Your score will appear on the daily leaderboard",
		"",
		"⌨️  Controls:",
		"• Type normally to start the challenge",
		"• Backspace to correct mistakes",
		"• Ctrl+C to quit anytime",
	}

	var rulesText []string
	for _, rule := range rules {
		if strings.HasPrefix(rule, "•") {
			rulesText = append(rulesText, rulesStyle.Render(rule))
		} else if strings.Contains(rule, "Rules:") || strings.Contains(rule, "Controls:") {
			rulesText = append(rulesText, highlightStyle.Render(rule))
		} else {
			rulesText = append(rulesText, rule)
		}
	}

	inputPrompt := instructionStyle.Render("Press Enter to confirm")
	inputStyle := lipgloss.NewStyle().Align(lipgloss.Center)
	centeredInput := inputStyle.Render(m.usernameInput.View())

	// Center everything if we have width
	if m.width > 0 {
		center := lipgloss.NewStyle().Width(m.width).Align(lipgloss.Center)
		return center.Render(lipgloss.JoinVertical(lipgloss.Left,
			"",
			title,
			instruction,
			"",
			strings.Join(rulesText, "\n"),
			"",
			centeredInput,
			"",
			inputPrompt,
		))
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		"",
		title,
		instruction,
		"",
		strings.Join(rulesText, "\n"),
		"",
		centeredInput,
		"",
		inputPrompt,
	)
}





package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	engineURL  = "http://127.0.0.1:8787/api/sessions?includeEvents=1"
	reportAddr = "127.0.0.1:8788"
)

type engineSession struct {
	ID                 string    `json:"id"`
	ProfileID          string    `json:"profileId"`
	Nickname           string    `json:"nickname"`
	Hotel              string    `json:"hotel"`
	CreatedAt          time.Time `json:"createdAt"`
	LoginMode          string    `json:"loginMode"`
	Status             string    `json:"status"`
	Automation         string    `json:"automation"`
	AutomationSeconds  int64     `json:"automationSeconds"`
	FishingDestination string    `json:"fishingDestination"`
	Events             []string  `json:"events"`
}

type capture struct {
	At        time.Time `json:"at"`
	AccountID string    `json:"accountId"`
	Account   string    `json:"account"`
	SessionID string    `json:"sessionId"`
	Fish      string    `json:"fish"`
	XP        int       `json:"xp"`
}

type accountReport struct {
	AccountID         string         `json:"accountId"`
	SessionID         string         `json:"sessionId"`
	Account           string         `json:"account"`
	LoginMode         string         `json:"loginMode"`
	Captures          int            `json:"captures"`
	XP                int            `json:"xp"`
	Casts             int            `json:"casts"`
	Timeouts          int            `json:"timeouts"`
	Pulls             int            `json:"pulls"`
	RoutesBlocked     int            `json:"routesBlocked"`
	TargetContentions int            `json:"targetContentions"`
	NoAckTimeouts     int            `json:"noAckTimeouts"`
	BiteTimeouts      int            `json:"biteTimeouts"`
	ResultTimeouts    int            `json:"resultTimeouts"`
	FishSlipped       int            `json:"fishSlipped"`
	AutomationSeconds int64          `json:"automationSeconds"`
	Sessions          int            `json:"sessions"`
	Fish              map[string]int `json:"fish"`
	LastCaptureAt     *time.Time     `json:"lastCaptureAt,omitempty"`
	CurrentStatus     string         `json:"currentStatus"`
	CurrentlyFishing  bool           `json:"currentlyFishing"`
	// FishingLevel só é preenchido quando o próprio hotel informar o nível.
	// Enquanto isso, os limites são uma confirmação segura pela área de pesca.
	FishingLevel              *int   `json:"fishingLevel,omitempty"`
	FishingLevelMin           int    `json:"fishingLevelMin,omitempty"`
	FishingLevelMax           int    `json:"fishingLevelMax,omitempty"`
	FishingLevelSource        string `json:"fishingLevelSource,omitempty"`
	FishingTotalXP            int    `json:"fishingTotalXp,omitempty"`
	FishingXPForCurrentLevel  int    `json:"fishingXpForCurrentLevel,omitempty"`
	FishingXPForNextLevel     int    `json:"fishingXpForNextLevel,omitempty"`
	FishingFishesCaught       int    `json:"fishingFishesCaught,omitempty"`
	FishingGoldenFishesCaught int    `json:"fishingGoldenFishesCaught,omitempty"`
}

type persisted struct {
	StartedAt       time.Time                 `json:"startedAt"`
	Accounts        map[string]*accountReport `json:"accounts"`
	Captures        []capture                 `json:"captures"`
	PreviousEvents  map[string][]string       `json:"previousEvents,omitempty"`
	SecondsSeen     map[string]int64          `json:"secondsSeen,omitempty"`
	SessionsSeen    map[string]bool           `json:"sessionsSeen,omitempty"`
	IdentifiedNames map[string]string         `json:"identifiedNames,omitempty"`
	SessionAccounts map[string]string         `json:"sessionAccounts,omitempty"`
}

type publicAccount struct {
	*accountReport
	FishPerMinute float64 `json:"fishPerMinute"`
	XPPerHour     float64 `json:"xpPerHour"`
	SuccessRate   float64 `json:"successRate"`
}

type publicReport struct {
	StartedAt time.Time       `json:"startedAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
	Totals    publicAccount   `json:"totals"`
	Accounts  []publicAccount `json:"accounts"`
	Captures  []capture       `json:"captures"`
}

type collector struct {
	mu           sync.RWMutex
	data         persisted
	dataPath     string
	lastSessions []engineSession
}

var capturePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)pegou um[a]?\s+(.+?)!.*?\+\s*(\d+)\s*EXP`),
	regexp.MustCompile(`(?i)caught (?:a|an)\s+(.+?)!.*?\+\s*(\d+)\s*EXP`),
}

var (
	exactFishingLevelPattern    = regexp.MustCompile(`(?i)(?:atingiu|alcançou|subiu(?: de nível)?|reached)\s+(?:o\s+)?(?:nível|level)\s*(\d{1,3})`)
	requiredFishingLevelPattern = regexp.MustCompile(`(?i)(?:precisa(?:\s+estar)?|necessita|need to be)\s+(?:no\s+)?(?:nível|level)\s*(\d{1,3})`)
	fishingStatsPattern         = regexp.MustCompile(`FISHING_STATS nivel=(\d+) maximo=(\d+) xpTotal=(\d+) xpNivelAtual=(\d+) xpProximoNivel=(\d+) peixes=(\d+) dourados=(\d+)`)
)

func main() {
	dataPath := filepath.Join("data", "fishing-reports.json")
	c := &collector{
		data:     newPersisted(time.Now()),
		dataPath: dataPath,
	}
	c.load()
	go c.loop()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/reports", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		_ = json.NewEncoder(w).Encode(c.snapshot())
	})
	mux.HandleFunc("POST /api/reports/reset", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		_ = json.NewEncoder(w).Encode(c.reset())
	})
	mux.HandleFunc("GET /api/reports/accounts/{id}/history", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		id := strings.TrimSpace(r.PathValue("id"))
		if id == "" {
			http.Error(w, `{"error":"sessão inválida"}`, http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(c.accountHistory(id))
	})
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	log.Printf("relatórios de pesca em http://%s", reportAddr)
	log.Fatal(http.ListenAndServe(reportAddr, mux))
}

func (c *collector) loop() {
	client := &http.Client{Timeout: 1200 * time.Millisecond}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		resp, err := client.Get(engineURL)
		if err != nil {
			continue
		}
		var sessions []engineSession
		err = json.NewDecoder(resp.Body).Decode(&sessions)
		_ = resp.Body.Close()
		if err != nil {
			continue
		}
		c.consume(sessions)
	}
}

func (c *collector) consume(sessions []engineSession) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastSessions = cloneSessions(sessions)
	changed := false
	for _, account := range c.data.Accounts {
		account.CurrentStatus = "fora da sessão"
		account.CurrentlyFishing = false
	}
	for _, s := range sessions {
		if identified := identifiedAccount(s.Events); identified != "" {
			if c.data.IdentifiedNames[s.ID] != identified {
				c.data.IdentifiedNames[s.ID] = identified
				changed = true
			}
		}
		if identified := c.data.IdentifiedNames[s.ID]; identified != "" {
			s.Nickname = identified
		}
		account := c.account(s)
		if account.Account != s.Nickname || account.LoginMode != s.LoginMode {
			account.Account = s.Nickname
			account.LoginMode = s.LoginMode
			for index := range c.data.Captures {
				if c.data.Captures[index].SessionID == s.ID {
					c.data.Captures[index].Account = s.Nickname
				}
			}
			changed = true
		}
		// As sessões são entregues em ordem de criação; uma reconexão mais nova
		// atualiza o cartão, sem deixar uma sessão antiga desconectada sobrescrever
		// o estado da mesma conta.
		if account.SessionID == s.ID || account.CurrentStatus == "fora da sessão" || !s.CreatedAt.Before(accountSessionCreatedAt(c.lastSessions, account.SessionID)) {
			account.SessionID = s.ID
			account.CurrentStatus = s.Status
		}
		account.CurrentlyFishing = account.CurrentlyFishing || s.Automation != ""
		if updateFishingLevelFromDestination(account, s.FishingDestination) {
			changed = true
		}
		if !c.data.SessionsSeen[s.ID] {
			c.data.SessionsSeen[s.ID] = true
			c.data.SecondsSeen[s.ID] = s.AutomationSeconds
			account.Sessions++
			changed = true
		} else if delta := s.AutomationSeconds - c.data.SecondsSeen[s.ID]; delta > 0 {
			account.AutomationSeconds += delta
			c.data.SecondsSeen[s.ID] = s.AutomationSeconds
			changed = true
		}

		previous := c.data.PreviousEvents[s.ID]
		newEvents := newEventsByOccurrence(previous, s.Events)
		for _, event := range newEvents {
			if c.consumeEvent(account, s, event) {
				changed = true
			}
		}
		c.data.PreviousEvents[s.ID] = append([]string(nil), s.Events...)
	}
	if changed {
		c.saveLocked()
	}
}

func identifiedAccount(events []string) string {
	const marker = "ACCOUNT_IDENTIFIED nome="
	for index := len(events) - 1; index >= 0; index-- {
		position := strings.Index(events[index], marker)
		if position >= 0 {
			return strings.TrimSpace(events[index][position+len(marker):])
		}
	}
	return ""
}

func (c *collector) consumeEvent(account *accountReport, session engineSession, event string) bool {
	if updateFishingLevelFromEvent(account, event) {
		return true
	}
	switch {
	case strings.Contains(event, "CAST_SENT"):
		account.Casts++
		return true
	case strings.Contains(event, "FISHING_TIMEOUT"):
		account.Timeouts++
		switch {
		case strings.Contains(event, "fase=lancamento-enviado"):
			account.NoAckTimeouts++
		case strings.Contains(event, "fase=aguardando-mordida"):
			account.BiteTimeouts++
		case strings.Contains(event, "fase=aguardando-resultado"):
			account.ResultTimeouts++
		}
		return true
	case strings.Contains(event, "MORDIDA_PUXADA"):
		account.Pulls++
		return true
	case strings.Contains(event, "ROTA_BLOQUEADA"), strings.Contains(event, "DESTINO_ALTERNATIVO"):
		account.RoutesBlocked++
		return true
	case strings.Contains(event, "FISHING_TARGET_BUSY"):
		account.TargetContentions++
		return true
	case strings.Contains(strings.ToLower(event), "fish slipped away"), strings.Contains(strings.ToLower(event), "peixe escapou"):
		account.FishSlipped++
		return true
	}
	for _, pattern := range capturePatterns {
		match := pattern.FindStringSubmatch(event)
		if len(match) != 3 {
			continue
		}
		xp, _ := strconv.Atoi(match[2])
		fish := strings.TrimSpace(match[1])
		now := time.Now()
		account.Captures++
		account.XP += xp
		account.Fish[fish]++
		account.LastCaptureAt = &now
		c.data.Captures = append(c.data.Captures, capture{now, account.AccountID, account.Account, session.ID, fish, xp})
		if len(c.data.Captures) > 2000 {
			c.data.Captures = c.data.Captures[len(c.data.Captures)-2000:]
		}
		return true
	}
	return false
}

func updateFishingLevelFromDestination(account *accountReport, destination string) bool {
	if account.FishingLevel != nil {
		return false
	}
	minimum := 0
	label := ""
	switch strings.ToLower(strings.TrimSpace(destination)) {
	case "jardim-flutuante":
		minimum, label = 30, "confirmado pela área Jardim Flutuante"
	case "snouthill-pier":
		minimum, label = 70, "confirmado pela área Snouthill Pier"
	}
	if minimum <= account.FishingLevelMin {
		return false
	}
	account.FishingLevelMin = minimum
	account.FishingLevelMax = 0
	account.FishingLevelSource = label
	return true
}

func updateFishingLevelFromEvent(account *accountReport, event string) bool {
	if match := fishingStatsPattern.FindStringSubmatch(event); len(match) == 8 {
		values := make([]int, 7)
		for index := range values {
			value, err := strconv.Atoi(match[index+1])
			if err != nil || value < 0 {
				return false
			}
			values[index] = value
		}
		if values[0] < 1 || values[0] > values[1] || values[1] > 100 {
			return false
		}
		changed := account.FishingLevel == nil || *account.FishingLevel != values[0] ||
			account.FishingTotalXP != values[2] || account.FishingXPForNextLevel != values[4] ||
			account.FishingFishesCaught != values[5] || account.FishingGoldenFishesCaught != values[6]
		level := values[0]
		account.FishingLevel = &level
		account.FishingLevelMin = level
		account.FishingLevelMax = level
		account.FishingLevelSource = "consulta direta ao hotel"
		account.FishingTotalXP = values[2]
		account.FishingXPForCurrentLevel = values[3]
		account.FishingXPForNextLevel = values[4]
		account.FishingFishesCaught = values[5]
		account.FishingGoldenFishesCaught = values[6]
		return changed
	}
	if match := exactFishingLevelPattern.FindStringSubmatch(event); len(match) == 2 {
		level, err := strconv.Atoi(match[1])
		if err == nil && level > 0 && level <= 100 && (account.FishingLevel == nil || *account.FishingLevel != level) {
			account.FishingLevel = &level
			account.FishingLevelMin = level
			account.FishingLevelMax = level
			account.FishingLevelSource = "informado pelo hotel"
			return true
		}
	}
	if match := requiredFishingLevelPattern.FindStringSubmatch(event); len(match) == 2 {
		required, err := strconv.Atoi(match[1])
		maximum := required - 1
		if err == nil && maximum > 0 && account.FishingLevel == nil && account.FishingLevelMin == 0 && (account.FishingLevelMax == 0 || maximum < account.FishingLevelMax) {
			account.FishingLevelMax = maximum
			account.FishingLevelSource = fmt.Sprintf("acesso negado em área de nível %d", required)
			return true
		}
	}
	return false
}

func (c *collector) account(session engineSession) *accountReport {
	accountID := stableAccountID(session)
	// Uma versão anterior do relatório identificava a conta apenas pelo apelido.
	// Quando o motor passa a informar o ProfileID, o apelido ainda é uma ponte
	// segura dentro do mesmo hotel (ele é único) para absorver esse histórico em
	// vez de criar um segundo cartão vazio.
	if strings.HasPrefix(accountID, "profile:") {
		if nicknameID := nicknameAccountID(session); nicknameID != "" && nicknameID != accountID {
			c.mergeAccountLocked(nicknameID, accountID)
		}
	}
	if previous := c.data.SessionAccounts[session.ID]; previous != "" && previous != accountID {
		// Uma conta pode iniciar sem apelido confirmado e ser identificada pouco
		// depois. Só movemos o grupo temporário daquela mesma sessão.
		if strings.HasPrefix(previous, "session:") {
			c.mergeAccountLocked(previous, accountID)
		}
	}
	c.data.SessionAccounts[session.ID] = accountID
	if existing := c.data.Accounts[accountID]; existing != nil {
		if existing.Account == "" || !isTemporaryNickname(session.Nickname) {
			existing.Account = session.Nickname
		}
		if session.LoginMode != "" {
			existing.LoginMode = session.LoginMode
		}
		return existing
	}
	created := &accountReport{
		AccountID: accountID,
		SessionID: session.ID,
		Account:   session.Nickname,
		LoginMode: session.LoginMode,
		Fish:      map[string]int{},
	}
	c.data.Accounts[accountID] = created
	return created
}

func stableAccountID(session engineSession) string {
	if profileID := strings.TrimSpace(session.ProfileID); profileID != "" {
		return "profile:" + profileID
	}
	if nicknameID := nicknameAccountID(session); nicknameID != "" {
		return nicknameID
	}
	return "session:" + session.ID
}

func nicknameAccountID(session engineSession) string {
	if nickname := normalizeAccountName(session.Nickname); nickname != "" && !isTemporaryNickname(session.Nickname) {
		hotel := normalizeAccountName(session.Hotel)
		if hotel == "" {
			hotel = "origins"
		}
		return "nick:" + hotel + ":" + nickname
	}
	return ""
}

func normalizeAccountName(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(value))), " ")
}

func isTemporaryNickname(value string) bool {
	return normalizeAccountName(value) == "" || normalizeAccountName(value) == "identificando conta…" || normalizeAccountName(value) == "identificando conta..."
}

func accountSessionCreatedAt(sessions []engineSession, sessionID string) time.Time {
	for _, session := range sessions {
		if session.ID == sessionID {
			return session.CreatedAt
		}
	}
	return time.Time{}
}

func (c *collector) mergeAccountLocked(fromID, toID string) {
	if fromID == toID {
		return
	}
	from := c.data.Accounts[fromID]
	if from == nil {
		return
	}
	to := c.data.Accounts[toID]
	if to == nil {
		from.AccountID = toID
		c.data.Accounts[toID] = from
		delete(c.data.Accounts, fromID)
		for index := range c.data.Captures {
			if c.data.Captures[index].AccountID == fromID {
				c.data.Captures[index].AccountID = toID
			}
		}
		for sessionID, accountID := range c.data.SessionAccounts {
			if accountID == fromID {
				c.data.SessionAccounts[sessionID] = toID
			}
		}
		return
	}
	to.Captures += from.Captures
	to.XP += from.XP
	to.Casts += from.Casts
	to.Timeouts += from.Timeouts
	to.Pulls += from.Pulls
	to.RoutesBlocked += from.RoutesBlocked
	to.TargetContentions += from.TargetContentions
	to.NoAckTimeouts += from.NoAckTimeouts
	to.BiteTimeouts += from.BiteTimeouts
	to.ResultTimeouts += from.ResultTimeouts
	to.FishSlipped += from.FishSlipped
	to.AutomationSeconds += from.AutomationSeconds
	to.Sessions += from.Sessions
	for fish, count := range from.Fish {
		to.Fish[fish] += count
	}
	if to.LastCaptureAt == nil || (from.LastCaptureAt != nil && from.LastCaptureAt.After(*to.LastCaptureAt)) {
		to.LastCaptureAt = from.LastCaptureAt
	}
	delete(c.data.Accounts, fromID)
	for index := range c.data.Captures {
		if c.data.Captures[index].AccountID == fromID {
			c.data.Captures[index].AccountID = toID
		}
	}
	for sessionID, accountID := range c.data.SessionAccounts {
		if accountID == fromID {
			c.data.SessionAccounts[sessionID] = toID
		}
	}
}

// newEventsByOccurrence encontra a sobreposição sequencial entre duas janelas
// de eventos. Comparar como multiconjunto apagava uma captura nova quando ela
// tinha exatamente o mesmo texto de uma captura antiga (por exemplo, dois
// "Você pegou um Tadpole!"). A sobreposição preserva a ordem do log e conta
// corretamente a repetição que acabou de entrar no fim da janela.
func newEventsByOccurrence(previous, current []string) []string {
	maxOverlap := min(len(previous), len(current))
	for overlap := maxOverlap; overlap > 0; overlap-- {
		matches := true
		for index := 0; index < overlap; index++ {
			if previous[len(previous)-overlap+index] != current[index] {
				matches = false
				break
			}
		}
		if matches {
			return append([]string(nil), current[overlap:]...)
		}
	}
	return append([]string(nil), current...)
}

func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func (c *collector) snapshot() publicReport {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.snapshotLocked()
}

func (c *collector) snapshotLocked() publicReport {
	accounts := make([]publicAccount, 0, len(c.data.Accounts))
	totals := &accountReport{Account: "Todas as contas", Fish: map[string]int{}}
	for _, account := range c.data.Accounts {
		copyAccount := *account
		copyAccount.Fish = cloneFish(account.Fish)
		accounts = append(accounts, withRates(&copyAccount))
		totals.Captures += account.Captures
		totals.XP += account.XP
		totals.Casts += account.Casts
		totals.Timeouts += account.Timeouts
		totals.Pulls += account.Pulls
		totals.RoutesBlocked += account.RoutesBlocked
		totals.TargetContentions += account.TargetContentions
		totals.NoAckTimeouts += account.NoAckTimeouts
		totals.BiteTimeouts += account.BiteTimeouts
		totals.ResultTimeouts += account.ResultTimeouts
		totals.FishSlipped += account.FishSlipped
		totals.AutomationSeconds += account.AutomationSeconds
		totals.Sessions += account.Sessions
		totals.CurrentlyFishing = totals.CurrentlyFishing || account.CurrentlyFishing
		for fish, count := range account.Fish {
			totals.Fish[fish] += count
		}
		if account.LastCaptureAt != nil && (totals.LastCaptureAt == nil || account.LastCaptureAt.After(*totals.LastCaptureAt)) {
			value := *account.LastCaptureAt
			totals.LastCaptureAt = &value
		}
	}
	// A ordem por número de capturas fazia cartões trocarem de posição a cada
	// atualização. Nome + ID deixam a lista estável durante toda a sessão.
	sort.SliceStable(accounts, func(i, j int) bool {
		left := strings.ToLower(strings.TrimSpace(accounts[i].Account))
		right := strings.ToLower(strings.TrimSpace(accounts[j].Account))
		if left == right {
			return accounts[i].AccountID < accounts[j].AccountID
		}
		return left < right
	})
	captures := make([]capture, len(c.data.Captures))
	copy(captures, c.data.Captures)
	for left, right := 0, len(captures)-1; left < right; left, right = left+1, right-1 {
		captures[left], captures[right] = captures[right], captures[left]
	}
	if len(captures) > 100 {
		captures = captures[:100]
	}
	return publicReport{c.data.StartedAt, time.Now(), withRates(totals), accounts, captures}
}

// accountHistory retorna somente a linha temporal da conta solicitada. Assim o
// dashboard detalha uma conta mesmo quando ela reconecta em outra sessão.
func (c *collector) accountHistory(accountID string) []capture {
	c.mu.RLock()
	defer c.mu.RUnlock()
	history := make([]capture, 0)
	for _, item := range c.data.Captures {
		if item.AccountID == accountID {
			history = append(history, item)
		}
	}
	sort.SliceStable(history, func(i, j int) bool { return history[i].At.Before(history[j].At) })
	if len(history) > 500 {
		history = history[len(history)-500:]
	}
	return history
}

// reset remove somente as métricas persistidas. As sessões e automações atuais
// seguem intactas. Os eventos e segundos já observados viram uma linha de base
// para que capturas ocorridas antes do clique não reapareçam no novo relatório.
func (c *collector) reset() publicReport {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = newPersisted(time.Now())
	for _, session := range c.lastSessions {
		c.data.PreviousEvents[session.ID] = append([]string(nil), session.Events...)
		c.data.SecondsSeen[session.ID] = session.AutomationSeconds
		c.data.SessionsSeen[session.ID] = true
		if identified := identifiedAccount(session.Events); identified != "" {
			c.data.IdentifiedNames[session.ID] = identified
		}
		c.data.SessionAccounts[session.ID] = stableAccountID(session)
	}
	c.saveLocked()
	return c.snapshotLocked()
}

func newPersisted(startedAt time.Time) persisted {
	return persisted{
		StartedAt:       startedAt,
		Accounts:        map[string]*accountReport{},
		PreviousEvents:  map[string][]string{},
		SecondsSeen:     map[string]int64{},
		SessionsSeen:    map[string]bool{},
		IdentifiedNames: map[string]string{},
		SessionAccounts: map[string]string{},
	}
}

func cloneSessions(source []engineSession) []engineSession {
	cloned := make([]engineSession, len(source))
	for index, session := range source {
		cloned[index] = session
		cloned[index].Events = append([]string(nil), session.Events...)
	}
	return cloned
}

func withRates(account *accountReport) publicAccount {
	minutes := float64(account.AutomationSeconds) / 60
	hours := float64(account.AutomationSeconds) / 3600
	fishPerMinute, xpPerHour, successRate := 0.0, 0.0, 0.0
	if minutes > 0 {
		fishPerMinute = float64(account.Captures) / minutes
	}
	if hours > 0 {
		xpPerHour = float64(account.XP) / hours
	}
	if account.Casts > 0 {
		successRate = float64(account.Captures) / float64(account.Casts) * 100
	}
	return publicAccount{account, round(fishPerMinute), round(xpPerHour), round(successRate)}
}

func round(value float64) float64 {
	parsed, _ := strconv.ParseFloat(fmt.Sprintf("%.2f", value), 64)
	return parsed
}

func cloneFish(source map[string]int) map[string]int {
	out := make(map[string]int, len(source))
	for key, value := range source {
		out[key] = value
	}
	return out
}

func (c *collector) load() {
	raw, err := os.ReadFile(c.dataPath)
	if err == nil {
		_ = json.Unmarshal(raw, &c.data)
	}
	if c.data.StartedAt.IsZero() {
		c.data.StartedAt = time.Now()
	}
	if c.data.Accounts == nil {
		c.data.Accounts = map[string]*accountReport{}
	}
	if c.data.PreviousEvents == nil {
		c.data.PreviousEvents = map[string][]string{}
	}
	if c.data.SecondsSeen == nil {
		c.data.SecondsSeen = map[string]int64{}
	}
	if c.data.SessionsSeen == nil {
		c.data.SessionsSeen = map[string]bool{}
	}
	if c.data.IdentifiedNames == nil {
		c.data.IdentifiedNames = map[string]string{}
	}
	if c.data.SessionAccounts == nil {
		c.data.SessionAccounts = map[string]string{}
	}
	// Relatórios das versões anteriores eram indexados pelo ID efêmero da
	// sessão. Normalizamos uma única vez para a identidade estável da conta,
	// preservando as capturas já registradas e evitando cartões duplicados após
	// uma reconexão.
	needsIdentityMigration := false
	for key, account := range c.data.Accounts {
		if account == nil || account.AccountID == "" || key != account.AccountID {
			needsIdentityMigration = true
			break
		}
	}
	if !needsIdentityMigration {
		for _, item := range c.data.Captures {
			if item.AccountID == "" {
				needsIdentityMigration = true
				break
			}
		}
	}
	if needsIdentityMigration {
		c.migrateAccountsByIdentity()
	}
	// A primeira versão persistia o histórico apenas pelo apelido. Se um
	// ProfileID já tiver sido salvo para aquele mesmo apelido, consolidamos os
	// dois registros durante a inicialização, inclusive sem uma sessão ativa.
	c.mergePersistedProfileAliases()
	for _, account := range c.data.Accounts {
		if account == nil {
			continue
		}
		if account.AccountID == "" {
			account.AccountID = legacyAccountID(account)
		}
		if account.Fish == nil {
			account.Fish = map[string]int{}
		}
		account.CurrentlyFishing = false
		account.CurrentStatus = ""
	}
}

func legacyAccountID(account *accountReport) string {
	if account == nil {
		return ""
	}
	if nickname := normalizeAccountName(account.Account); nickname != "" && !isTemporaryNickname(account.Account) {
		return "nick:origins:" + nickname
	}
	return "session:" + account.SessionID
}

func (c *collector) migrateAccountsByIdentity() {
	rebuilt := make(map[string]*accountReport, len(c.data.Accounts))
	for _, source := range c.data.Accounts {
		if source == nil {
			continue
		}
		identity := strings.TrimSpace(source.AccountID)
		if identity == "" {
			identity = legacyAccountID(source)
		}
		if identity == "session:" {
			identity = "session:" + source.SessionID
		}
		source.AccountID = identity
		if source.Fish == nil {
			source.Fish = map[string]int{}
		}
		if source.SessionID != "" {
			c.data.SessionAccounts[source.SessionID] = identity
		}
		if current := rebuilt[identity]; current != nil {
			mergeReportMetrics(current, source)
			continue
		}
		copyAccount := *source
		copyAccount.Fish = cloneFish(source.Fish)
		if source.LastCaptureAt != nil {
			value := *source.LastCaptureAt
			copyAccount.LastCaptureAt = &value
		}
		rebuilt[identity] = &copyAccount
	}
	c.data.Accounts = rebuilt
	for index := range c.data.Captures {
		item := &c.data.Captures[index]
		if item.AccountID != "" {
			continue
		}
		if accountID := c.data.SessionAccounts[item.SessionID]; accountID != "" {
			item.AccountID = accountID
			continue
		}
		if nickname := normalizeAccountName(item.Account); nickname != "" && !isTemporaryNickname(item.Account) {
			item.AccountID = "nick:origins:" + nickname
			continue
		}
		item.AccountID = "session:" + item.SessionID
	}
}

func mergeReportMetrics(destination, source *accountReport) {
	if destination == nil || source == nil {
		return
	}
	destination.Captures += source.Captures
	destination.XP += source.XP
	destination.Casts += source.Casts
	destination.Timeouts += source.Timeouts
	destination.Pulls += source.Pulls
	destination.RoutesBlocked += source.RoutesBlocked
	destination.TargetContentions += source.TargetContentions
	destination.NoAckTimeouts += source.NoAckTimeouts
	destination.BiteTimeouts += source.BiteTimeouts
	destination.ResultTimeouts += source.ResultTimeouts
	destination.FishSlipped += source.FishSlipped
	destination.AutomationSeconds += source.AutomationSeconds
	destination.Sessions += source.Sessions
	for fish, count := range source.Fish {
		destination.Fish[fish] += count
	}
	if destination.LastCaptureAt == nil || (source.LastCaptureAt != nil && source.LastCaptureAt.After(*destination.LastCaptureAt)) {
		if source.LastCaptureAt != nil {
			value := *source.LastCaptureAt
			destination.LastCaptureAt = &value
		}
	}
	if destination.Account == "" || isTemporaryNickname(destination.Account) {
		destination.Account = source.Account
	}
	if destination.LoginMode == "" {
		destination.LoginMode = source.LoginMode
	}
	if destination.SessionID == "" {
		destination.SessionID = source.SessionID
	}
}

func (c *collector) mergePersistedProfileAliases() {
	profilesByNickname := make(map[string]string)
	for accountID, account := range c.data.Accounts {
		if account == nil || !strings.HasPrefix(accountID, "profile:") {
			continue
		}
		if nickname := normalizeAccountName(account.Account); nickname != "" && !isTemporaryNickname(account.Account) {
			profilesByNickname[nickname] = accountID
		}
	}
	for accountID, account := range c.data.Accounts {
		if account == nil || !strings.HasPrefix(accountID, "nick:") {
			continue
		}
		nickname := normalizeAccountName(account.Account)
		if targetID := profilesByNickname[nickname]; targetID != "" && targetID != accountID {
			c.mergeAccountLocked(accountID, targetID)
		}
	}
}

func (c *collector) saveLocked() {
	if err := os.MkdirAll(filepath.Dir(c.dataPath), 0o755); err != nil {
		return
	}
	raw, err := json.MarshalIndent(c.data, "", "  ")
	if err != nil {
		return
	}
	temporary := c.dataPath + ".tmp"
	if os.WriteFile(temporary, raw, 0o600) == nil {
		_ = os.Rename(temporary, c.dataPath)
	}
}

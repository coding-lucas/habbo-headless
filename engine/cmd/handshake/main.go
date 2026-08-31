package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"math"
	mathrand "math/rand"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"habbo-headless/engine/internal/origins"
)

const (
	hello             = 0
	secretKey         = 1
	cryptoParameters  = 277
	initCrypto        = 206
	generateKey       = 202
	versionCheck      = 5
	uniqueID          = 6
	getSessionParams  = 181
	sessionParams     = 257
	infoRetrieve      = 7
	userObject        = 5
	loginIncorrect    = 33
	noLoginPermission = 20
	ping              = 50
	pong              = 196
	steamLogin        = 764
	steamOpenIDLogin  = 770
	steamOpenIDLink   = 284
	rights            = 2
	loginOK           = 3
	tryLogin          = 4
	// O cliente Origins atual registra TRY_LOGIN neste cabeçalho e envia
	// somente e-mail e senha. Contas antigas ainda aceitam o cabeçalho 4.
	tryLoginCurrent         = 756
	userBanned              = 35
	totpRequired            = 1575
	navigate                = 150
	navNodeInfo             = 220
	searchBusyRooms         = 13
	searchRoomText          = 17
	flatResults             = 16
	roomSearchResults       = 55
	getRecommendedRooms     = 264
	recommendedRoomList     = 351
	roomDirectory           = 2
	tryFlat                 = 57
	goToFlat                = 59
	flatLetIn               = 41
	roomNotAllowed          = 131
	opcOK                   = 19
	quitRoom                = 53
	dance                   = 93
	getInterstitial         = 182
	roomReady               = 69
	stopAction              = 88
	getRoomAd               = 126
	getHeightMap            = 60
	getUsers                = 61
	getObjects              = 62
	getItems                = 63
	getStatus               = 64
	usersInRoom             = 28
	activeObjects           = 32
	activeObjectAdd         = 93
	activeObjectRemove      = 94
	statusUpdate            = 34
	floorHeightmap          = 31
	moveAvatar              = 1269
	chat                    = 52
	shout                   = 55
	startFishing            = 1100
	fishingChat             = 1101
	fishingRodLevel         = 1105
	fishingStats            = 1106
	startFishingAck         = 1107
	fishingStatus           = 1108
	endFishing              = 1109
	wave                    = 94
	globalChatCommandPrefix = "chat:send:"
	// No cliente gráfico, o STARTFISHING pode disparar uma aproximação
	// automática a distâncias maiores. Na sessão headless isso não é garantido:
	// o servidor simplesmente ignora vários lançamentos acima de ~3 quadrados.
	// Usar a faixa segura do bot original evita ficar relançando sem sair do lugar.
	alcancePesca              = 2.5
	intervaloReforcoPuxada    = 1200 * time.Millisecond
	maximoReforcosPuxada      = 1
	timeoutAguardandoMordida  = 25 * time.Second
	timeoutResultadoPuxada    = 12 * time.Second
	timeoutPescaSemResposta   = 4200 * time.Millisecond
	cooldownPeixeSemResposta  = 12 * time.Second
	cooldownPeixeOcupado      = 2500 * time.Millisecond
	cooldownResultadoPerdido  = 20 * time.Second
	intervaloRevisaoMovimento = 750 * time.Millisecond
	tentativasAntesDesvio     = 6
	maxResyncsSemPeixe        = 4
)

type fishingPhase string

const (
	fishingPhaseSeeking     fishingPhase = "buscando-alvo"
	fishingPhaseMoving      fishingPhase = "movendo"
	fishingPhaseCastSent    fishingPhase = "lancamento-enviado"
	fishingPhaseWaitingBite fishingPhase = "aguardando-mordida"
	fishingPhasePullSent    fishingPhase = "aguardando-resultado"
)

func fishingTimeoutForPhase(phase fishingPhase) time.Duration {
	switch phase {
	case fishingPhaseWaitingBite:
		return timeoutAguardandoMordida
	case fishingPhasePullSent:
		return timeoutResultadoPuxada
	default:
		return timeoutPescaSemResposta
	}
}

func fishingTimeoutCooldown(phase fishingPhase) time.Duration {
	switch phase {
	case fishingPhasePullSent:
		return cooldownResultadoPerdido
	case fishingPhaseWaitingBite:
		return cooldownPeixeSemResposta
	default:
		return cooldownPeixeSemResposta
	}
}

type loginConfig struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	TOTP     string `json:"totp"`
}

const fishingTargetLeaseDuration = 45 * time.Second

// fishingCoordinatorClient conversa somente com o motor local. Em caso de
// indisponibilidade ele falha aberto: uma falha do painel nunca pode impedir
// uma conta de pescar sozinha.
type fishingCoordinatorClient struct {
	baseURL         string
	sessionID       string
	client          *http.Client
	failureReported bool
}

type coordinatorLeaseRequest struct {
	SessionID string `json:"sessionId"`
	RoomKey   string `json:"roomKey"`
	TargetID  int    `json:"targetId"`
	LeaseMS   int    `json:"leaseMs"`
}

type coordinatorLeaseResponse struct {
	Reserved bool `json:"reserved"`
}

type coordinatorReleaseRequest struct {
	SessionID string `json:"sessionId"`
	RoomKey   string `json:"roomKey"`
	TargetID  int    `json:"targetId"`
}

type coordinatorRoomCandidate struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Users    int    `json:"users"`
	Capacity int    `json:"capacity"`
}

type coordinatorRoomClaimRequest struct {
	SessionID   string                     `json:"sessionId"`
	Destination string                     `json:"destination"`
	Candidates  []coordinatorRoomCandidate `json:"candidates"`
}

type coordinatorRoomClaimResponse struct {
	RoomKey string `json:"roomKey"`
}

func newFishingCoordinatorClient() *fishingCoordinatorClient {
	return &fishingCoordinatorClient{
		baseURL:   strings.TrimRight(os.Getenv("HABBO_FISHING_COORDINATOR_URL"), "/"),
		sessionID: strings.TrimSpace(os.Getenv("HABBO_SESSION_ID")),
		client:    &http.Client{Timeout: 180 * time.Millisecond},
	}
}

func (c *fishingCoordinatorClient) enabled() bool {
	return c != nil && c.baseURL != "" && c.sessionID != ""
}

func (c *fishingCoordinatorClient) post(path string, input any, output any) error {
	if !c.enabled() {
		return nil
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return err
	}
	request, err := http.NewRequest(http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("coordenador respondeu %s", response.Status)
	}
	if output != nil {
		return json.NewDecoder(response.Body).Decode(output)
	}
	return nil
}

func (c *fishingCoordinatorClient) reportFailure(operation string) {
	if c == nil || c.failureReported {
		return
	}
	c.failureReported = true
	fmt.Printf("MARCO_OK: FISHING_COORDINATOR_INDISPONIVEL operacao=%s\n", operation)
}

func (c *fishingCoordinatorClient) reserveTarget(roomKey string, targetID int) bool {
	if !c.enabled() {
		return true
	}
	var output coordinatorLeaseResponse
	err := c.post("/api/fishing/lease", coordinatorLeaseRequest{
		SessionID: c.sessionID,
		RoomKey:   roomKey,
		TargetID:  targetID,
		LeaseMS:   int(fishingTargetLeaseDuration / time.Millisecond),
	}, &output)
	if err != nil {
		c.reportFailure("reservar-alvo")
		return true
	}
	return output.Reserved
}

func (c *fishingCoordinatorClient) releaseTarget(roomKey string, targetID int) {
	if !c.enabled() || targetID <= 0 {
		return
	}
	if err := c.post("/api/fishing/release", coordinatorReleaseRequest{SessionID: c.sessionID, RoomKey: roomKey, TargetID: targetID}, nil); err != nil {
		c.reportFailure("liberar-alvo")
	}
}

func (c *fishingCoordinatorClient) chooseRoom(destination string, rooms []publicRoom) publicRoom {
	if len(rooms) == 0 {
		return publicRoom{}
	}
	// Fallback local determinístico para versões antigas do motor ou uma
	// inicialização parcial do coordenador.
	fallback := append([]publicRoom(nil), rooms...)
	sort.SliceStable(fallback, func(i, j int) bool {
		left, right := fallback[i], fallback[j]
		leftLoad, rightLoad := roomOccupancy(left), roomOccupancy(right)
		if leftLoad != rightLoad {
			return leftLoad < rightLoad
		}
		if left.Capacity != right.Capacity {
			return left.Capacity > right.Capacity
		}
		return fishingRoomKey(destination, left) < fishingRoomKey(destination, right)
	})
	if !c.enabled() {
		return fallback[0]
	}
	candidates := make([]coordinatorRoomCandidate, 0, len(fallback))
	for _, room := range fallback {
		candidates = append(candidates, coordinatorRoomCandidate{
			Key: fishingRoomKey(destination, room), Name: room.Name, Users: room.Users, Capacity: room.Capacity,
		})
	}
	var output coordinatorRoomClaimResponse
	err := c.post("/api/fishing/room-claim", coordinatorRoomClaimRequest{
		SessionID: c.sessionID, Destination: destination, Candidates: candidates,
	}, &output)
	if err != nil {
		c.reportFailure("escolher-sala")
		return fallback[0]
	}
	for _, room := range fallback {
		if fishingRoomKey(destination, room) == output.RoomKey {
			return room
		}
	}
	return fallback[0]
}

func (c *fishingCoordinatorClient) releaseRoom() {
	if !c.enabled() {
		return
	}
	if err := c.post("/api/fishing/room-release", struct {
		SessionID string `json:"sessionId"`
	}{SessionID: c.sessionID}, nil); err != nil {
		c.reportFailure("liberar-sala")
	}
}

func fishingRoomKey(destination string, room publicRoom) string {
	return fmt.Sprintf("%s:%d:%d", destination, room.Port, room.Door)
}

func roomOccupancy(room publicRoom) int {
	if room.Capacity <= 0 {
		return room.Users * 100
	}
	return room.Users * 100 / room.Capacity
}

func main() {
	loginMode := strings.ToLower(os.Getenv("HABBO_LOGIN_MODE"))
	input := bufio.NewReader(os.Stdin)
	var credentials loginConfig
	if loginMode == "habbo" {
		line, err := input.ReadBytes('\n')
		if err != nil || json.Unmarshal(line, &credentials) != nil {
			log.Fatal("não foi possível receber a autenticação em memória")
		}
		if loginMode == "habbo" && (credentials.Email == "" || credentials.Password == "") {
			log.Fatal("e-mail e senha são obrigatórios")
		}
	}
	address := "game-obr.habbo.com:40001"
	if len(os.Args) > 1 {
		address = os.Args[1]
	}
	conn, err := net.DialTimeout("tcp", address, 8*time.Second)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
	log.Printf("TCP conectado a %s", conn.RemoteAddr())

	cryptoState, err := origins.NewCrypto()
	if err != nil {
		log.Fatal(err)
	}
	buffer := &origins.PlainServerBuffer{}
	chunk := make([]byte, 8192)
	stage := "hello"
	for {
		n, err := conn.Read(chunk)
		if err != nil {
			log.Fatal(err)
		}
		buffer.Push(chunk[:n])
		for {
			packet, ok := buffer.Next()
			if !ok {
				break
			}
			header, err := origins.Header(packet)
			if err != nil {
				log.Fatal(err)
			}
			log.Printf("recebido header=%d bytes=%d", header, len(packet)-2)
			switch {
			case stage == "hello" && header == hello:
				if err := sendPlain(conn, initCrypto); err != nil {
					log.Fatal(err)
				}
				stage = "parameters"
			case stage == "parameters" && header == cryptoParameters:
				key, _ := origins.OutgoingString(cryptoState.PublicKey())
				if err := sendPlain(conn, generateKey, key); err != nil {
					log.Fatal(err)
				}
				stage = "secret"
			case stage == "secret" && header == secretKey:
				serverKey, err := origins.IncomingString(packet, 2)
				if err != nil {
					log.Fatal(err)
				}
				if err := cryptoState.SetPeer(serverKey); err != nil {
					log.Fatal(err)
				}
				fmt.Println("MARCO_OK: negociação Diffie-Hellman concluída; quatro fluxos ChaCha20 derivados")
				if err := sendEncrypted(conn, cryptoState, versionCheck,
					origins.EncodeVL64(1139),
					mustString("1"),
					mustString("http://origins-gamedata.habbo.com.br/external_variables/1"),
				); err != nil {
					log.Fatal(err)
				}
				machine := make([]byte, 16)
				if _, err := rand.Read(machine); err != nil {
					log.Fatal(err)
				}
				if err := sendEncrypted(conn, cryptoState, uniqueID, mustString(fmt.Sprintf("%x", machine))); err != nil {
					log.Fatal(err)
				}
				if err := sendEncrypted(conn, cryptoState, getSessionParams); err != nil {
					log.Fatal(err)
				}
				reader := newEncryptedPacketReader(conn, cryptoState)
				if err := awaitSessionParameters(reader); err != nil {
					log.Fatal(err)
				}
				fmt.Println("MARCO_OK: SESSION_PARAMETERS recebido sem cliente gráfico")
				if loginMode == "habbo" {
					if err := sendEncrypted(conn, cryptoState, tryLogin, loginPayload(credentials)...); err != nil {
						log.Fatal(err)
					}
					fmt.Println("MARCO_OK: TRY_LOGIN enviado no formato Origins validado de quatro campos")
					if err := awaitLoginOK(conn, cryptoState, reader, credentials); err != nil {
						log.Fatal(err)
					}
					// SetDeadline foi usado apenas para limitar o handshake inicial.
					// Sem limpar também o prazo de escrita, o próximo PONG enviado
					// depois de 20 segundos falha com "write: i/o timeout" e derruba
					// exclusivamente as contas autenticadas por e-mail/senha.
					_ = conn.SetDeadline(time.Time{})
					credentials = loginConfig{}
				} else if loginMode == "steam" {
					if err := sendEncrypted(conn, cryptoState, steamOpenIDLogin); err != nil {
						log.Fatal(err)
					}
					link, err := awaitOpenIDLink(conn, cryptoState, reader)
					if err != nil {
						log.Fatal(err)
					}
					fmt.Println("MARCO_OK: link OpenID oficial recebido e mantido somente em memória")
					fmt.Printf("AUTH_OPENID_URL: %s\n", link)
					_ = conn.SetDeadline(time.Time{})
					fmt.Println("AÇÃO_NECESSÁRIA: abra a autorização oficial pelo dashboard")
					if err := awaitLoginOK(conn, cryptoState, reader, loginConfig{}); err != nil {
						log.Fatal(err)
					}
				} else {
					log.Fatal("modo de login não suportado")
				}
				accountName := requestOwnProfile(conn, cryptoState, reader)
				if accountName != "" {
					fmt.Printf("MARCO_OK: ACCOUNT_IDENTIFIED nome=%s\n", accountName)
				}
				fmt.Println("MARCO_OK: LOGIN_OK recebido; sessão headless autenticada")
				if err := controlLoop(conn, cryptoState, reader, input, accountName); err != nil {
					log.Fatal(err)
				}
				return
			}
		}
	}
}

func controlLoop(conn net.Conn, cryptoState *origins.Crypto, reader *encryptedPacketReader, input *bufio.Reader, accountName string) error {
	commands := make(chan string)
	go func() {
		scanner := bufio.NewScanner(input)
		for scanner.Scan() {
			commands <- strings.TrimSpace(scanner.Text())
		}
		close(commands)
	}()
	for {
		select {
		case command, ok := <-commands:
			if !ok {
				return fmt.Errorf("canal de controle encerrado")
			}
			switch {
			case strings.HasPrefix(command, globalChatCommandPrefix):
				if _, err := handleGlobalChatCommand(conn, cryptoState, command); err != nil {
					return err
				}
			case strings.HasPrefix(command, "fishing:start:"):
				destination := strings.TrimPrefix(command, "fishing:start:")
				_ = conn.SetDeadline(time.Time{})
				for {
					err := enterFishingRoom(conn, cryptoState, reader, commands, accountName, destination)
					if err == errFishingStopped {
						break
					}
					if err == errFishingRoomStale {
						fmt.Printf("MARCO_OK: FISHING_ROOM_RECOVERY destino=%s reencontrando-quarto\n", destination)
						continue
					}
					if err != nil {
						return err
					}
					break
				}
			case command == "fishing:stop":
				if err := sendEncrypted(conn, cryptoState, stopAction); err != nil {
					return err
				}
				fmt.Println("MARCO_OK: FISHING_STOPPED")
			case command == "rooms:refresh":
				if err := refreshBusyRooms(conn, cryptoState, reader); err != nil {
					fmt.Printf("MARCO_OK: ROOM_CATALOG_ERROR erro=%s\n", err)
				}
			case strings.HasPrefix(command, "rooms:owner:"):
				owner, ok := parseRoomOwnerSearchCommand(command)
				if !ok {
					fmt.Println("MARCO_OK: ROOM_CATALOG_ERROR erro=consulta por dono inválida")
					continue
				}
				if err := searchRoomsByOwner(conn, cryptoState, reader, owner); err != nil {
					fmt.Printf("MARCO_OK: ROOM_CATALOG_ERROR erro=%s\n", err)
				}
			case strings.HasPrefix(command, "formation:start:"):
				roomID, port, door, config, err := parseFormationStartCommand(command)
				if err != nil {
					fmt.Printf("MARCO_OK: FORMATION_ERROR erro=%s\n", err)
					continue
				}
				if err := enterPartyRoom(conn, cryptoState, reader, commands, accountName, roomID, port, door, &config); err != nil {
					if err == errAutomationStopped {
						continue
					}
					fmt.Printf("MARCO_OK: FORMATION_ERROR erro=%s\n", err)
				}
			case strings.HasPrefix(command, "party:start:"):
				roomID, port, door, err := parsePartyRoomCommand(command)
				if err != nil || roomID <= 0 {
					fmt.Println("MARCO_OK: PARTY_ERROR quarto inválido")
					continue
				}
				if err := enterPartyRoom(conn, cryptoState, reader, commands, accountName, roomID, port, door, nil); err != nil {
					if err == errAutomationStopped {
						continue
					}
					// Uma falha de entrada não pode derrubar a conta. Mantemos a
					// sessão pronta para outra seleção de quarto no dashboard.
					fmt.Printf("MARCO_OK: PARTY_ERROR erro=%s\n", err)
					continue
				}
			case isAutomationStop(command):
				if err := sendEncrypted(conn, cryptoState, quitRoom); err != nil {
					return err
				}
				fmt.Println("MARCO_OK: AUTOMATION_STOPPED")
			}
		default:
			_ = conn.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
			packet, err := reader.Next()
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue
				}
				return err
			}
			header, err := origins.Header(packet)
			if err != nil {
				return err
			}
			if header == ping {
				if err := replyPong(conn, cryptoState, packet); err != nil {
					return err
				}
			}
		}
	}
}

func pollFishingStop(conn net.Conn, cryptoState *origins.Crypto, commands <-chan string) (bool, error) {
	select {
	case command, ok := <-commands:
		if !ok {
			return false, fmt.Errorf("canal de controle encerrado")
		}
		if isAutomationStop(command) {
			if err := quitRoomImmediately(conn, cryptoState); err != nil {
				return false, err
			}
			fmt.Println("MARCO_OK: FISHING_STOPPED")
			return true, nil
		}
		if _, err := handleGlobalChatCommand(conn, cryptoState, command); err != nil {
			return false, err
		}
	default:
	}
	return false, nil
}

func isAutomationStop(command string) bool {
	return command == "automation:stop" || command == "fishing:stop" || command == "party:stop"
}

// A parada do painel é operacional, não uma animação: interrompe a ação e
// sai da sala no mesmo instante. Caminhar até a porta mantinha sessões
// ocupadas e atrasava uma nova automação.
func quitRoomImmediately(conn net.Conn, cryptoState *origins.Crypto) error {
	if err := sendEncrypted(conn, cryptoState, stopAction); err != nil {
		return err
	}
	if err := sendEncrypted(conn, cryptoState, quitRoom); err != nil {
		return err
	}
	fmt.Println("MARCO_OK: ROOM_EXITED_IMMEDIATELY")
	return nil
}

func decodeGlobalChatCommand(command string) (string, bool, error) {
	if !strings.HasPrefix(command, globalChatCommandPrefix) {
		return "", false, nil
	}
	encoded := strings.TrimPrefix(command, globalChatCommandPrefix)
	data, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return "", true, fmt.Errorf("mensagem global corrompida")
	}
	message := strings.TrimSpace(string(data))
	if message == "" || len([]rune(message)) > 100 || strings.ContainsAny(message, "\r\n\x00\x02") {
		return "", true, fmt.Errorf("mensagem global inválida")
	}
	return message, true, nil
}

func handleGlobalChatCommand(conn net.Conn, cryptoState *origins.Crypto, command string) (bool, error) {
	message, handled, err := decodeGlobalChatCommand(command)
	if err != nil || !handled {
		return handled, err
	}
	// SHOUT é o mesmo envio feito pelo botão "Gritar" do cliente gráfico.
	// CHAT só produz a fala local e não atende ao envio global do painel.
	if err := sendEncrypted(conn, cryptoState, shout, mustString(message)); err != nil {
		return true, err
	}
	fmt.Println("MARCO_OK: GLOBAL_CHAT_SENT")
	return true, nil
}

type publicRoom struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Owner       string `json:"owner,omitempty"`
	Access      string `json:"access,omitempty"`
	Users       int    `json:"users"`
	Capacity    int    `json:"capacity"`
	Description string `json:"description,omitempty"`
	Port        int    `json:"port,omitempty"`
	Door        int    `json:"door,omitempty"`
}

type fishingRoomConfig struct {
	ID      string
	Label   string
	Aliases []string
}

var fishingRooms = map[string]fishingRoomConfig{
	"infobus": {
		ID: "infobus", Label: "Infobus", Aliases: []string{"infobus"},
	},
	"jardim-flutuante": {
		ID: "jardim-flutuante", Label: "Jardim Flutuante",
		Aliases: []string{"jardim flutuante", "floating garden", "jardin flotante", "port hana", "porto hana"},
	},
	"snouthill-pier": {
		ID: "snouthill-pier", Label: "Snouthill Pier", Aliases: []string{"snouthill"},
	},
}

var errFishingStopped = fmt.Errorf("pesca interrompida pelo controle")
var errAutomationStopped = fmt.Errorf("automação interrompida pelo controle")
var errFishingRoomStale = fmt.Errorf("sala de pesca sem dados após ressincronizações")

type formationConfig struct {
	Slot  int
	Total int
}

var formationPatternNames = map[string]string{
	"coracao": "Coração grande",
	"estrela": "Estrela de cinco pontas",
	"coroa":   "Coroa",
	"peixe":   "Peixe",
	"trofeu":  "Troféu",
	"smile":   "Carinha sorrindo",
	"help":    "Palavra HELP",
}

func isFormationShape(shape string) bool {
	_, ok := formationPatternNames[shape]
	return ok
}

func refreshBusyRooms(conn net.Conn, cryptoState *origins.Crypto, reader *encryptedPacketReader) error {
	// O cliente gráfico mantém duas árvores no navegador: a pública (3) e a
	// de flats de jogadores (4). Percorremos ambas para que o painel ofereça
	// destinos fixos do hotel — como a Recepção — sem esconder os quartos de
	// Habbos encontrados na árvore privada.
	rooms := make(map[int]publicRoom)
	queue := []int{3, 4}
	visited := make(map[int]bool)
	for len(queue) > 0 && len(visited) < 96 {
		nodeID := queue[0]
		queue = queue[1:]
		if nodeID <= 0 || visited[nodeID] {
			continue
		}
		visited[nodeID] = true
		if err := sendEncrypted(conn, cryptoState, navigate, origins.EncodeVL64(0), origins.EncodeVL64(nodeID), origins.EncodeVL64(2)); err != nil {
			return err
		}
		deadline := time.Now().Add(1800 * time.Millisecond)
		gotNode := false
		for time.Now().Before(deadline) {
			_ = conn.SetReadDeadline(time.Now().Add(350 * time.Millisecond))
			packet, err := reader.Next()
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			if err != nil {
				return err
			}
			header, err := origins.Header(packet)
			if err != nil {
				return err
			}
			if header == ping {
				if err := replyPong(conn, cryptoState, packet); err != nil {
					return err
				}
				continue
			}
			if header == recommendedRoomList {
				if recommended, err := parseRecommendedRoomList(packet[2:]); err == nil {
					for _, room := range recommended {
						rooms[room.ID] = room
					}
				}
				continue
			}
			if header != navNodeInfo {
				continue
			}
			nodes, err := parseNavigatorNodes(packet[2:])
			if err != nil {
				return err
			}
			for _, node := range nodes {
				if node.Room != nil && node.Room.ID > 0 {
					rooms[node.Room.ID] = *node.Room
				}
				for _, room := range node.Rooms {
					if room.ID > 0 {
						rooms[room.ID] = room
					}
				}
				if node.Type == 0 && node.ID > 0 && !visited[node.ID] {
					queue = append(queue, node.ID)
				}
			}
			gotNode = true
			break
		}
		if !gotNode {
			log.Printf("navegador: nó %d não respondeu", nodeID)
		}
	}
	if len(rooms) == 0 {
		return fmt.Errorf("o navegador não devolveu quartos disponíveis")
	}
	items := make([]publicRoom, 0, len(rooms))
	for _, room := range rooms {
		items = append(items, room)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Users == items[j].Users {
			return items[i].Name < items[j].Name
		}
		return items[i].Users > items[j].Users
	})
	return emitRooms(items)
}

func searchRoomsByOwner(conn net.Conn, cryptoState *origins.Crypto, reader *encryptedPacketReader, owner string) error {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return fmt.Errorf("informe o nome do Habbo dono do quarto")
	}
	if len([]rune(owner)) > 64 || strings.ContainsAny(owner, "\r\n\x00") {
		return fmt.Errorf("nome de Habbo inválido")
	}
	// SRCHF é a pesquisa global da aba de quartos do navegador oficial. Ela
	// devolve FLAT_RESULTS de pesquisa (cabeçalho 55), diferente de SUSERF,
	// que devolve apenas os quartos da própria conta conectada.
	if err := sendEncrypted(conn, cryptoState, searchRoomText, mustString(owner)); err != nil {
		return err
	}
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(400 * time.Millisecond))
		packet, err := reader.Next()
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			continue
		}
		if err != nil {
			return err
		}
		header, err := origins.Header(packet)
		if err != nil {
			return err
		}
		if header == ping {
			if err := replyPong(conn, cryptoState, packet); err != nil {
				return err
			}
			continue
		}
		if header != roomSearchResults && header != flatResults {
			continue
		}
		return emitRoomCatalog(packet[2:])
	}
	return fmt.Errorf("a busca pelo dono %q não respondeu a tempo", owner)
}

func parseRoomOwnerSearchCommand(command string) (string, bool) {
	const prefix = "rooms:owner:"
	if !strings.HasPrefix(command, prefix) {
		return "", false
	}
	encoded := strings.TrimPrefix(command, prefix)
	decoded, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return "", false
	}
	return string(decoded), true
}

func emitRoomCatalog(payload []byte) error {
	rooms, err := parseFlatResults(payload)
	if err != nil {
		return err
	}
	fmt.Println("MARCO_OK: ROOM_CATALOG_BEGIN")
	for _, room := range rooms {
		data, _ := json.Marshal(room)
		fmt.Printf("ROOM_CATALOG: %s\n", data)
	}
	fmt.Printf("MARCO_OK: ROOM_CATALOG_DONE total=%d\n", len(rooms))
	return nil
}

func emitRecommendedRoomCatalog(payload []byte) error {
	rooms, err := parseRecommendedRoomList(payload)
	if err != nil {
		return err
	}
	return emitRooms(rooms)
}

func emitRooms(rooms []publicRoom) error {
	fmt.Println("MARCO_OK: ROOM_CATALOG_BEGIN")
	for _, room := range rooms {
		data, err := json.Marshal(room)
		if err != nil {
			return err
		}
		fmt.Printf("ROOM_CATALOG: %s\n", data)
	}
	fmt.Printf("MARCO_OK: ROOM_CATALOG_DONE total=%d\n", len(rooms))
	return nil
}

func parseRecommendedRoomList(payload []byte) ([]publicRoom, error) {
	c := &packetCursor{data: payload}
	count, err := c.integer()
	if err != nil {
		return nil, err
	}
	if count < 0 || count > 100 {
		return nil, fmt.Errorf("quantidade de quartos recomendados inválida: %d", count)
	}
	rooms := make([]publicRoom, 0, count)
	for i := 0; i < count; i++ {
		id, err := c.integer()
		if err != nil {
			return nil, err
		}
		name, err := c.str()
		if err != nil {
			return nil, err
		}
		owner, err := c.str()
		if err != nil {
			return nil, err
		}
		access, err := c.str()
		if err != nil {
			return nil, err
		}
		users, err := c.integer()
		if err != nil {
			return nil, err
		}
		capacity, err := c.integer()
		if err != nil {
			return nil, err
		}
		description, err := c.str()
		if err != nil {
			return nil, err
		}
		rooms = append(rooms, publicRoom{
			ID: id, Name: name, Owner: owner, Access: strings.ToLower(strings.TrimSpace(access)),
			Users: users, Capacity: capacity, Description: description,
		})
	}
	sort.SliceStable(rooms, func(i, j int) bool {
		if rooms[i].Users == rooms[j].Users {
			return rooms[i].Name < rooms[j].Name
		}
		return rooms[i].Users > rooms[j].Users
	})
	return rooms, nil
}

func parseFlatResults(payload []byte) ([]publicRoom, error) {
	content := strings.TrimRight(string(payload), "\x00\x02")
	rooms := make([]publicRoom, 0)
	for _, line := range strings.Split(content, "\r") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 9 {
			continue
		}
		id, errID := strconv.Atoi(fields[0])
		users, errUsers := strconv.Atoi(fields[5])
		capacity, errCapacity := strconv.Atoi(fields[6])
		if errID != nil || errUsers != nil || errCapacity != nil {
			continue
		}
		rooms = append(rooms, publicRoom{
			ID: id, Name: fields[1], Owner: fields[2], Access: fields[3],
			Users: users, Capacity: capacity, Description: fields[8],
		})
	}
	sort.SliceStable(rooms, func(i, j int) bool {
		if rooms[i].Users == rooms[j].Users {
			return rooms[i].Name < rooms[j].Name
		}
		return rooms[i].Users > rooms[j].Users
	})
	return rooms, nil
}

func parsePartyRoomCommand(command string) (roomID, port, door int, err error) {
	parts := strings.Split(strings.TrimPrefix(command, "party:start:"), ":")
	if len(parts) != 1 && len(parts) != 3 {
		return 0, 0, 0, fmt.Errorf("formato de quarto inválido")
	}
	roomID, err = strconv.Atoi(parts[0])
	if err != nil || len(parts) == 1 {
		return roomID, 0, 0, err
	}
	port, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, 0, err
	}
	door, err = strconv.Atoi(parts[2])
	return roomID, port, door, err
}

func parseFormationStartCommand(command string) (roomID, port, door int, config formationConfig, err error) {
	parts := strings.Split(strings.TrimPrefix(command, "formation:start:"), ":")
	if len(parts) != 5 {
		return 0, 0, 0, formationConfig{}, fmt.Errorf("formato de formação inválido")
	}
	values := make([]int, len(parts))
	for index, part := range parts {
		values[index], err = strconv.Atoi(part)
		if err != nil {
			return 0, 0, 0, formationConfig{}, fmt.Errorf("formato de formação inválido")
		}
	}
	roomID, port, door = values[0], values[1], values[2]
	config = formationConfig{Slot: values[3], Total: values[4]}
	if roomID <= 0 || config.Total != 35 || config.Slot < 0 || config.Slot >= config.Total {
		return 0, 0, 0, formationConfig{}, fmt.Errorf("a formação exige exatamente 35 posições válidas")
	}
	return roomID, port, door, config, nil
}

func enterPartyRoom(conn net.Conn, cryptoState *origins.Crypto, reader *encryptedPacketReader, commands <-chan string, accountName string, roomID, port, door int, formation *formationConfig) error {
	roomText := strconv.Itoa(roomID)
	publicRoom := port > 0 && door >= 0
	if publicRoom {
		if err := sendEncrypted(conn, cryptoState, getInterstitial, mustString("general")); err != nil {
			return err
		}
		if err := sendEncrypted(conn, cryptoState, roomDirectory, origins.EncodeVL64(1), origins.EncodeVL64(port), origins.EncodeVL64(door)); err != nil {
			return err
		}
	} else {
		// A entrada privada começa pelo ROOM_DIRECTORY com tipo 0, ID do flat e
		// porta 0. TRYFLAT só é usado pelo cliente depois do OPC_OK dessa etapa.
		if err := sendEncrypted(conn, cryptoState, roomDirectory, origins.EncodeVL64(0), origins.EncodeVL64(roomID), origins.EncodeVL64(0)); err != nil {
			return err
		}
	}
	fmt.Printf("MARCO_OK: PARTY_ROOM_SELECTED id=%d\n", roomID)
	tryFlatSent := false
	goToFlatSent := false
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case command, ok := <-commands:
			if !ok {
				return fmt.Errorf("canal de controle encerrado")
			}
			if handled, err := handleGlobalChatCommand(conn, cryptoState, command); handled {
				if err != nil {
					return err
				}
				continue
			}
			if isAutomationStop(command) {
				if err := quitRoomImmediately(conn, cryptoState); err != nil {
					return err
				}
				fmt.Println("MARCO_OK: AUTOMATION_STOPPED")
				return errAutomationStopped
			}
		default:
		}
		_ = conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		packet, err := reader.Next()
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			continue
		}
		if err != nil {
			return err
		}
		header, err := origins.Header(packet)
		if err != nil {
			return err
		}
		switch header {
		case ping:
			if err := replyPong(conn, cryptoState, packet); err != nil {
				return err
			}
		case opcOK:
			fmt.Printf("MARCO_OK: PARTY_OPC_OK id=%d\n", roomID)
			// Fluxo oficial de quarto privado: ROOM_DIRECTORY -> OPC_OK ->
			// TRYFLAT -> FLAT_LETIN -> GOTOFLAT -> ROOM_READY.
			if !publicRoom && !tryFlatSent {
				// A implementação Origins atual aceita o ROOM_DIRECTORY em VL64,
				// mas exige o identificador bruto do flat nesta continuação. Este
				// é o mesmo formato do bot G-Earth que já confirmou entradas.
				if err := sendEncrypted(conn, cryptoState, tryFlat, []byte(roomText)); err != nil {
					return err
				}
				tryFlatSent = true
				fmt.Printf("MARCO_OK: PARTY_TRYFLAT_SENT id=%d\n", roomID)
			}
		case flatLetIn:
			fmt.Printf("MARCO_OK: PARTY_FLAT_LETIN id=%d\n", roomID)
			if !publicRoom && tryFlatSent && !goToFlatSent {
				if err := sendEncrypted(conn, cryptoState, goToFlat, []byte(roomText)); err != nil {
					return err
				}
				goToFlatSent = true
				fmt.Printf("MARCO_OK: PARTY_GOTOFLAT_SENT id=%d\n", roomID)
			}
		case roomReady:
			fmt.Printf("MARCO_OK: PARTY_ROOM_READY id=%d\n", roomID)
			if formation != nil {
				fmt.Printf("MARCO_OK: FORMATION_ROOM_READY id=%d posição=%d/%d\n", roomID, formation.Slot+1, formation.Total)
				return runFormation(conn, cryptoState, reader, commands, accountName, roomID, *formation)
			}
			return runParty(conn, cryptoState, reader, commands, accountName, roomID)
		case roomNotAllowed:
			return fmt.Errorf("o quarto %d recusou a entrada", roomID)
		}
	}
	return fmt.Errorf("o quarto %d não confirmou a entrada", roomID)
}

func runParty(conn net.Conn, cryptoState *origins.Crypto, reader *encryptedPacketReader, commands <-chan string, accountName string, roomID int) error {
	steps := []struct {
		header  int
		payload []byte
	}{
		{stopAction, mustString("CarryItem")}, {getRoomAd, nil}, {getHeightMap, nil},
		{getUsers, nil}, {getObjects, nil}, {getItems, nil}, {getStatus, nil},
	}
	for _, step := range steps {
		if err := sendEncrypted(conn, cryptoState, step.header, step.payload); err != nil {
			return err
		}
		time.Sleep(70 * time.Millisecond)
	}

	var heightmap []string
	ownIndex, currentX, currentY := -1, 0, 0
	doorX, doorY, doorKnown := 0, 0, false
	nextMove := time.Now().Add(2 * time.Second)
	nextDance := time.Now().Add(500 * time.Millisecond)
	rng := mathrand.New(mathrand.NewSource(time.Now().UnixNano() + int64(roomID)))
	fmt.Printf("MARCO_OK: PARTY_STARTED id=%d\n", roomID)

	for {
		select {
		case command, ok := <-commands:
			if !ok {
				return fmt.Errorf("canal de controle encerrado")
			}
			if handled, err := handleGlobalChatCommand(conn, cryptoState, command); handled {
				if err != nil {
					return err
				}
				continue
			}
			if isAutomationStop(command) {
				if err := quitRoomImmediately(conn, cryptoState); err != nil {
					return err
				}
				fmt.Println("MARCO_OK: PARTY_STOPPED")
				fmt.Println("MARCO_OK: AUTOMATION_STOPPED")
				return errAutomationStopped
			}
		default:
		}

		if doorKnown && time.Now().After(nextDance) {
			_ = sendEncrypted(conn, cryptoState, dance)
			nextDance = time.Now().Add(time.Duration(22+rng.Intn(18)) * time.Second)
			fmt.Println("MARCO_OK: PARTY_DANCING")
		}
		if doorKnown && len(heightmap) > 0 && time.Now().After(nextMove) {
			if x, y, ok := randomPartyTile(rng, heightmap, currentX, currentY, doorX, doorY); ok {
				_ = sendEncrypted(conn, cryptoState, moveAvatar, origins.EncodeVL64(x), origins.EncodeVL64(y), origins.EncodeVL64(0))
				fmt.Printf("MARCO_OK: PARTY_WALKING destino=%d,%d\n", x, y)
			}
			nextMove = time.Now().Add(time.Duration(3+rng.Intn(5)) * time.Second)
		}

		_ = conn.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
		packet, err := reader.Next()
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			continue
		}
		if err != nil {
			return err
		}
		header, err := origins.Header(packet)
		if err != nil {
			return err
		}
		switch header {
		case ping:
			if err := replyPong(conn, cryptoState, packet); err != nil {
				return err
			}
		case floorHeightmap:
			raw := strings.TrimRight(string(packet[2:]), "\x00\x02")
			heightmap = strings.Split(raw, "\r")
		case usersInRoom:
			entities, parseErr := parseRoomUsers(packet[2:])
			if parseErr != nil {
				continue
			}
			for _, entity := range entities {
				if entity.kind == 1 && strings.EqualFold(entity.name, accountName) {
					ownIndex, currentX, currentY = entity.index, entity.x, entity.y
					if !doorKnown {
						doorX, doorY, doorKnown = currentX, currentY, true
						fmt.Printf("MARCO_OK: ROOM_DOOR_MAPPED x=%d y=%d\n", doorX, doorY)
					}
				}
			}
			if ownIndex < 0 && shouldUseSingleUserFallback(accountName, ownIndex, entities) {
				ownIndex, currentX, currentY = entities[0].index, entities[0].x, entities[0].y
				doorX, doorY, doorKnown = currentX, currentY, true
			}
		case statusUpdate:
			statuses, parseErr := parseEntityStatuses(packet[2:])
			if parseErr != nil {
				continue
			}
			for _, status := range statuses {
				if status.index == ownIndex {
					currentX, currentY = status.x, status.y
				}
			}
		}
	}
}

type formationCell struct{ x, y int }

func formationRows(shape string) ([]string, error) {
	switch shape {
	case "fila":
		return []string{"#######", "#######", "#######", "#######", "#######"}, nil
	case "coracao":
		return []string{"###.###", "#######", "#######", ".#####.", "#######", "..###.."}, nil
	case "estrela":
		return []string{"##...##", "###.###", ".#####.", "#######", ".#####.", ".#####.", ".#.#.#."}, nil
	case "coroa":
		return []string{"#.#.#.#", "#######", ".#####.", "#######", "#######", ".#####."}, nil
	case "peixe":
		return []string{"..#####..", ".#######.", "#########", ".#######.", "..#####..", "#.......#"}, nil
	case "trofeu":
		return []string{".#####.", "#######", ".#####.", "..###..", "..###..", ".#####.", "#######"}, nil
	case "smile":
		return []string{".#####.", "#######", "##.#.##", "#######", "##...##", "###.###", "...#..."}, nil
	case "help":
		glyphs := [][]string{
			{"#.#", "#.#", "###", "#.#", "#.#"},
			{"###", "#..", "##.", "#..", "###"},
			{"#..", "#..", "#..", "#..", "###"},
			{"##.", "#.#", "##.", "#..", "..."},
		}
		rows := make([]string, 5)
		for y := range rows {
			parts := make([]string, 0, len(glyphs))
			for _, glyph := range glyphs {
				parts = append(parts, glyph[y])
			}
			rows[y] = strings.Join(parts, ".")
		}
		return rows, nil
	default:
		return nil, fmt.Errorf("figura de formação inválida")
	}
}

func formationCellsFor(shape string) ([]formationCell, int, int, error) {
	rows, err := formationRows(shape)
	if err != nil {
		return nil, 0, 0, err
	}
	width := 0
	cells := make([]formationCell, 0, 35)
	for y, row := range rows {
		if len(row) > width {
			width = len(row)
		}
		for x := range row {
			if row[x] == '#' {
				cells = append(cells, formationCell{x: x, y: y})
			}
		}
	}
	if len(cells) != 35 {
		return nil, 0, 0, fmt.Errorf("a figura %s possui %d posições; eram esperadas 35", shape, len(cells))
	}
	return cells, width, len(rows), nil
}

func formationTarget(heightmap []string, shape string, slot int, doorX, doorY int) (int, int, bool) {
	cells, width, height, err := formationCellsFor(shape)
	if err != nil || slot < 0 || slot >= len(cells) || len(heightmap) < height {
		return 0, 0, false
	}
	roomWidth := 0
	for _, row := range heightmap {
		if len(row) > roomWidth {
			roomWidth = len(row)
		}
	}
	bestX, bestY, bestScore := 0, 0, math.MaxFloat64
	for originY := 0; originY+height <= len(heightmap); originY++ {
		for originX := 0; originX+width <= roomWidth; originX++ {
			valid := true
			for _, cell := range cells {
				x, y := originX+cell.x, originY+cell.y
				if y >= len(heightmap) || x >= len(heightmap[y]) || heightmap[y][x] == 'x' || heightmap[y][x] == 'X' || (x == doorX && y == doorY) {
					valid = false
					break
				}
			}
			if !valid {
				continue
			}
			score := distancia(originX+width/2, originY+height/2, roomWidth/2, len(heightmap)/2)
			if score < bestScore {
				bestX, bestY, bestScore = originX, originY, score
			}
		}
	}
	if math.IsInf(bestScore, 1) {
		return 0, 0, false
	}
	target := cells[slot]
	return bestX + target.x, bestY + target.y, true
}

func runFormation(conn net.Conn, cryptoState *origins.Crypto, reader *encryptedPacketReader, commands <-chan string, accountName string, roomID int, config formationConfig) error {
	steps := []struct {
		header  int
		payload []byte
	}{
		{stopAction, mustString("CarryItem")}, {getRoomAd, nil}, {getHeightMap, nil}, {getUsers, nil}, {getStatus, nil},
	}
	for _, step := range steps {
		if err := sendEncrypted(conn, cryptoState, step.header, step.payload); err != nil {
			return err
		}
		time.Sleep(70 * time.Millisecond)
	}

	shape := "fila"
	var heightmap []string
	ownIndex, currentX, currentY := -1, 0, 0
	doorX, doorY, doorKnown := 0, 0, false
	nextMove := time.Now().Add(time.Duration(350+config.Slot*70) * time.Millisecond)
	lastShape, lastX, lastY := "", -1, -1
	noSpaceReported := false
	fmt.Printf("MARCO_OK: FORMATION_QUEUE posição=%d/%d sala=%d\n", config.Slot+1, config.Total, roomID)

	for {
		select {
		case command, ok := <-commands:
			if !ok {
				return fmt.Errorf("canal de controle encerrado")
			}
			if handled, err := handleGlobalChatCommand(conn, cryptoState, command); handled {
				if err != nil {
					return err
				}
				continue
			}
			if isAutomationStop(command) {
				if err := quitRoomImmediately(conn, cryptoState); err != nil {
					return err
				}
				fmt.Println("MARCO_OK: FORMATION_STOPPED")
				fmt.Println("MARCO_OK: AUTOMATION_STOPPED")
				return errAutomationStopped
			}
			if nextShape := strings.TrimPrefix(command, "formation:shape:"); nextShape != command {
				if !isFormationShape(nextShape) {
					fmt.Println("MARCO_OK: FORMATION_ERROR erro=figura inválida")
					continue
				}
				shape = nextShape
				lastShape, lastX, lastY = "", -1, -1
				noSpaceReported = false
				fmt.Printf("MARCO_OK: FORMATION_SHAPE figura=%s posição=%d/%d\n", formationPatternNames[shape], config.Slot+1, config.Total)
			}
		default:
		}

		if doorKnown && ownIndex >= 0 && len(heightmap) > 0 && time.Now().After(nextMove) {
			targetX, targetY, found := formationTarget(heightmap, shape, config.Slot, doorX, doorY)
			if !found {
				if !noSpaceReported {
					fmt.Printf("MARCO_OK: FORMATION_NO_SPACE figura=%s\n", shape)
					noSpaceReported = true
				}
			} else if currentX != targetX || currentY != targetY {
				if err := sendEncrypted(conn, cryptoState, moveAvatar, origins.EncodeVL64(targetX), origins.EncodeVL64(targetY), origins.EncodeVL64(0)); err != nil {
					return err
				}
				if lastShape != shape || lastX != targetX || lastY != targetY {
					fmt.Printf("MARCO_OK: FORMATION_MOVING figura=%s destino=%d,%d posição=%d/%d\n", shape, targetX, targetY, config.Slot+1, config.Total)
				}
				lastShape, lastX, lastY = shape, targetX, targetY
			}
			nextMove = time.Now().Add(1500 * time.Millisecond)
		}

		_ = conn.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
		packet, err := reader.Next()
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			continue
		}
		if err != nil {
			return err
		}
		header, err := origins.Header(packet)
		if err != nil {
			return err
		}
		switch header {
		case ping:
			if err := replyPong(conn, cryptoState, packet); err != nil {
				return err
			}
		case floorHeightmap:
			raw := strings.TrimRight(string(packet[2:]), "\x00\x02")
			heightmap = strings.Split(raw, "\r")
		case usersInRoom:
			entities, parseErr := parseRoomUsers(packet[2:])
			if parseErr != nil {
				continue
			}
			for _, entity := range entities {
				if entity.kind == 1 && strings.EqualFold(entity.name, accountName) {
					ownIndex, currentX, currentY = entity.index, entity.x, entity.y
					if !doorKnown {
						doorX, doorY, doorKnown = currentX, currentY, true
					}
				}
			}
			if ownIndex < 0 && shouldUseSingleUserFallback(accountName, ownIndex, entities) {
				ownIndex, currentX, currentY = entities[0].index, entities[0].x, entities[0].y
				doorX, doorY, doorKnown = currentX, currentY, true
			}
		case statusUpdate:
			statuses, parseErr := parseEntityStatuses(packet[2:])
			if parseErr != nil {
				continue
			}
			for _, status := range statuses {
				if status.index == ownIndex {
					currentX, currentY = status.x, status.y
				}
			}
		}
	}
}

func randomPartyTile(rng *mathrand.Rand, heightmap []string, currentX, currentY, doorX, doorY int) (int, int, bool) {
	tiles := make([][2]int, 0)
	for y, row := range heightmap {
		for x := range row {
			if row[x] == 'x' || row[x] == 'X' || (x == doorX && y == doorY) {
				continue
			}
			distance := distancia(currentX, currentY, x, y)
			if distance >= 2 && distance <= 9 {
				tiles = append(tiles, [2]int{x, y})
			}
		}
	}
	if len(tiles) == 0 {
		return 0, 0, false
	}
	tile := tiles[rng.Intn(len(tiles))]
	return tile[0], tile[1], true
}

func walkToDoorAndQuit(conn net.Conn, cryptoState *origins.Crypto, reader *encryptedPacketReader, ownIndex, currentX, currentY, doorX, doorY int, doorKnown bool) error {
	if doorKnown && (currentX != doorX || currentY != doorY) {
		if err := sendEncrypted(conn, cryptoState, moveAvatar, origins.EncodeVL64(doorX), origins.EncodeVL64(doorY), origins.EncodeVL64(0)); err != nil {
			return err
		}
		fmt.Printf("MARCO_OK: EXIT_WALK_STARTED porta=%d,%d\n", doorX, doorY)
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			_ = conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
			packet, err := reader.Next()
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			if err != nil {
				return err
			}
			header, err := origins.Header(packet)
			if err != nil {
				return err
			}
			if header == ping {
				if err := replyPong(conn, cryptoState, packet); err != nil {
					return err
				}
				continue
			}
			if header == statusUpdate {
				statuses, _ := parseEntityStatuses(packet[2:])
				for _, status := range statuses {
					if status.index == ownIndex {
						currentX, currentY = status.x, status.y
					}
				}
				if currentX == doorX && currentY == doorY {
					break
				}
			}
		}
	}
	if err := sendEncrypted(conn, cryptoState, quitRoom); err != nil {
		return err
	}
	fmt.Println("MARCO_OK: ROOM_EXITED")
	_ = conn.SetReadDeadline(time.Time{})
	return nil
}

type navigatorNode struct {
	ID       int
	Type     int
	ParentID int
	Name     string
	Room     *publicRoom
	Rooms    []publicRoom
}

func enterFishingRoom(conn net.Conn, cryptoState *origins.Crypto, reader *encryptedPacketReader, commands <-chan string, accountName, destination string) error {
	config, ok := fishingRooms[destination]
	if !ok {
		return fmt.Errorf("destino de pesca desconhecido: %s", destination)
	}
	coordinator := newFishingCoordinatorClient()
	target, err := findPublicFishingRoom(conn, cryptoState, reader, commands, config, coordinator)
	if err != nil {
		return err
	}
	fmt.Printf("MARCO_OK: FISHING_ROOM_SELECTED destino=%s nome=%s\n", config.ID, target.Name)
	log.Printf("quarto público localizado: %s", target.Name)
	if err := sendEncrypted(conn, cryptoState, getInterstitial, mustString("general")); err != nil {
		return err
	}
	if err := sendEncrypted(conn, cryptoState, roomDirectory, origins.EncodeVL64(1), origins.EncodeVL64(target.Port), origins.EncodeVL64(target.Door)); err != nil {
		return err
	}
	for {
		stopped, err := pollFishingStop(conn, cryptoState, commands)
		if err != nil {
			return err
		}
		if stopped {
			return errFishingStopped
		}
		_ = conn.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
		packet, err := reader.Next()
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			return err
		}
		header, err := origins.Header(packet)
		if err != nil {
			return err
		}
		if header == ping {
			if err := replyPong(conn, cryptoState, packet); err != nil {
				return err
			}
		} else if header == roomReady {
			fmt.Printf("MARCO_OK: FISHING_ROOM_READY destino=%s nome=%s\n", config.ID, target.Name)
			if strings.EqualFold(os.Getenv("HABBO_FISHING_DRIVER"), "gearth") {
				return initializeRoomForGEarth(conn, cryptoState, reader, commands)
			}
			return initializeRoomAndFish(conn, cryptoState, reader, commands, accountName, config, fishingRoomKey(config.ID, *target), coordinator)
		}
	}
}

func findPublicFishingRoom(conn net.Conn, cryptoState *origins.Crypto, reader *encryptedPacketReader, commands <-chan string, config fishingRoomConfig, coordinator *fishingCoordinatorClient) (*publicRoom, error) {
	queue := []int{3}
	visited := map[int]bool{}
	for len(queue) > 0 && len(visited) < 32 {
		nodeID := queue[0]
		queue = queue[1:]
		if visited[nodeID] {
			continue
		}
		visited[nodeID] = true
		if err := sendEncrypted(conn, cryptoState, navigate, origins.EncodeVL64(0), origins.EncodeVL64(nodeID), origins.EncodeVL64(2)); err != nil {
			return nil, err
		}
		for {
			stopped, err := pollFishingStop(conn, cryptoState, commands)
			if err != nil {
				return nil, err
			}
			if stopped {
				return nil, errFishingStopped
			}
			_ = conn.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
			packet, err := reader.Next()
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue
				}
				return nil, err
			}
			header, err := origins.Header(packet)
			if err != nil {
				return nil, err
			}
			if header == ping {
				if err := replyPong(conn, cryptoState, packet); err != nil {
					return nil, err
				}
				continue
			}
			if header != navNodeInfo {
				continue
			}
			nodes, err := parseNavigatorNodes(packet[2:])
			if err != nil {
				return nil, err
			}
			matches := make([]publicRoom, 0)
			for _, node := range nodes {
				if node.Room != nil && matchesFishingRoom(node.Room.Name, config) {
					matches = append(matches, *node.Room)
				}
				if node.Type == 0 && node.ID > 0 && !visited[node.ID] {
					queue = append(queue, node.ID)
				}
			}
			if len(matches) > 0 {
				chosen := coordinator.chooseRoom(config.ID, matches)
				return &chosen, nil
			}
			break
		}
	}
	return nil, fmt.Errorf("o quarto %s não apareceu no navegador público", config.Label)
}

func initializeRoomForGEarth(conn net.Conn, cryptoState *origins.Crypto, reader *encryptedPacketReader, commands <-chan string) error {
	steps := []struct {
		header  int
		payload []byte
	}{
		{stopAction, mustString("CarryItem")}, {getRoomAd, nil}, {getHeightMap, nil},
		{getUsers, nil}, {getObjects, nil}, {getItems, nil}, {getStatus, nil},
	}
	for _, step := range steps {
		if err := sendEncrypted(conn, cryptoState, step.header, step.payload); err != nil {
			return err
		}
		time.Sleep(80 * time.Millisecond)
	}
	// Diferente do estado do minijogo, as estatísticas são devolvidas pelo
	// hotel somente após uma solicitação explícita. Essa consulta é apenas de
	// leitura e não altera a pesca assumida pela extensão G-Earth.
	if err := requestFishingStats(conn, cryptoState); err != nil {
		return err
	}
	nextStatsRefresh := time.Now().Add(2 * time.Minute)

	// A extensão V72 intercepta e bloqueia :p no G-Earth. Portanto o texto
	// nunca chega ao chat do hotel e serve somente como canal de controle.
	if err := sendEncrypted(conn, cryptoState, chat, mustString(":p")); err != nil {
		return err
	}
	active := true
	roomCatalogPending := false
	var roomCatalogDeadline time.Time
	fmt.Println("MARCO_OK: GEARTH_FISHING_STARTED extensão V72 assumiu a pesca")

	for {
		if time.Now().After(nextStatsRefresh) {
			if err := requestFishingStats(conn, cryptoState); err != nil {
				return err
			}
			nextStatsRefresh = time.Now().Add(2 * time.Minute)
		}
		if roomCatalogPending && time.Now().After(roomCatalogDeadline) {
			fmt.Println("MARCO_OK: ROOM_CATALOG_ERROR erro=o navegador não respondeu a tempo")
			roomCatalogPending = false
		}
		select {
		case command, ok := <-commands:
			if !ok {
				return fmt.Errorf("canal de controle encerrado")
			}
			if handled, err := handleGlobalChatCommand(conn, cryptoState, command); handled {
				if err != nil {
					return err
				}
				continue
			}
			switch command {
			case "fishing:start":
				if !active {
					if err := sendEncrypted(conn, cryptoState, chat, mustString(":p")); err != nil {
						return err
					}
					active = true
					fmt.Println("MARCO_OK: GEARTH_FISHING_STARTED")
				}
			case "fishing:stop":
				if active {
					if err := sendEncrypted(conn, cryptoState, chat, mustString(":p")); err != nil {
						return err
					}
					active = false
					fmt.Println("MARCO_OK: GEARTH_FISHING_STOPPED")
				}
			case "rooms:refresh":
				if err := refreshBusyRooms(conn, cryptoState, reader); err != nil {
					fmt.Printf("MARCO_OK: ROOM_CATALOG_ERROR erro=%s\n", err)
				}
			default:
				if owner, ok := parseRoomOwnerSearchCommand(command); ok {
					if err := searchRoomsByOwner(conn, cryptoState, reader, owner); err != nil {
						fmt.Printf("MARCO_OK: ROOM_CATALOG_ERROR erro=%s\n", err)
					}
				}
			}
		default:
		}

		_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		packet, err := reader.Next()
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			continue
		}
		if err != nil {
			return err
		}
		header, err := origins.Header(packet)
		if err != nil {
			return err
		}
		if header == ping {
			if err := replyPong(conn, cryptoState, packet); err != nil {
				return err
			}
		} else if header == fishingStats {
			if err := emitFishingStats(packet[2:]); err != nil {
				fmt.Printf("MARCO_OK: FISHING_STATS_ERROR erro=%s\n", err)
			}
		} else if header == recommendedRoomList && roomCatalogPending {
			if err := emitRecommendedRoomCatalog(packet[2:]); err != nil {
				fmt.Printf("MARCO_OK: ROOM_CATALOG_ERROR erro=%s\n", err)
			}
			roomCatalogPending = false
		}
	}
}

type fishingStatistics struct {
	CurrentLevel       int
	MaximumLevel       int
	TotalXP            int
	XPForCurrentLevel  int
	XPForNextLevel     int
	FishesCaught       int
	GoldenFishesCaught int
}

func requestFishingStats(conn net.Conn, cryptoState *origins.Crypto) error {
	if err := sendEncrypted(conn, cryptoState, fishingStats); err != nil {
		return err
	}
	// A segunda consulta disponibiliza o nível da vara no cliente oficial.
	// Não é exibida no painel por enquanto, mas mantém a leitura compatível
	// com o fluxo normal da interface de pesca.
	if err := sendEncrypted(conn, cryptoState, fishingRodLevel); err != nil {
		return err
	}
	fmt.Println("MARCO_OK: FISHING_STATS_REQUESTED")
	return nil
}

func parseFishingStats(payload []byte) (fishingStatistics, error) {
	cursor := &packetCursor{data: payload}
	values := make([]int, 7)
	for index := range values {
		value, err := cursor.integer()
		if err != nil {
			return fishingStatistics{}, fmt.Errorf("campo %d: %w", index+1, err)
		}
		values[index] = value
	}
	// O cliente oficial consome os sete campos principais. Versões mais novas
	// do hotel podem acrescentar metadados ao fim do pacote; preservamos os
	// campos conhecidos para não perder a leitura de nível por isso.
	return fishingStatistics{
		CurrentLevel:       values[0],
		MaximumLevel:       values[1],
		TotalXP:            values[2],
		XPForCurrentLevel:  values[3],
		XPForNextLevel:     values[4],
		FishesCaught:       values[5],
		GoldenFishesCaught: values[6],
	}, nil
}

func emitFishingStats(payload []byte) error {
	stats, err := parseFishingStats(payload)
	if err != nil {
		return err
	}
	fmt.Printf("MARCO_OK: FISHING_STATS nivel=%d maximo=%d xpTotal=%d xpNivelAtual=%d xpProximoNivel=%d peixes=%d dourados=%d\n",
		stats.CurrentLevel,
		stats.MaximumLevel,
		stats.TotalXP,
		stats.XPForCurrentLevel,
		stats.XPForNextLevel,
		stats.FishesCaught,
		stats.GoldenFishesCaught,
	)
	return nil
}

func initializeRoomAndFish(conn net.Conn, cryptoState *origins.Crypto, reader *encryptedPacketReader, commands <-chan string, accountName string, room fishingRoomConfig, roomKey string, coordinator *fishingCoordinatorClient) error {
	steps := []struct {
		header  int
		payload []byte
	}{
		{stopAction, mustString("CarryItem")}, {getRoomAd, nil}, {getHeightMap, nil},
		{getUsers, nil}, {getObjects, nil}, {getItems, nil}, {getStatus, nil},
	}
	for _, step := range steps {
		if err := sendEncrypted(conn, cryptoState, step.header, step.payload); err != nil {
			return err
		}
		time.Sleep(80 * time.Millisecond)
	}
	// O driver nativo é o responsável pela pesca efetiva no Headless. Solicita
	// as estatísticas imediatamente após os dados da sala estarem disponíveis.
	if err := requestFishingStats(conn, cryptoState); err != nil {
		return err
	}
	nextStatsRefresh := time.Now().Add(2 * time.Minute)
	_ = conn.SetReadDeadline(time.Time{})
	fmt.Printf("MARCO_OK: aguardando área de pesca em %s\n", room.Label)
	areas := map[int]fishingArea{}
	meuX, meuY, alvo, alvoX, alvoY, destX, destY := 0, 0, 0, 0, 0, 0, 0
	positionKnown := false
	ownIndex := -1
	doorX, doorY, doorKnown := 0, 0, false
	entityNames := map[int]string{}
	identifiedAccount := accountName
	usersPackets := 0
	var heightmap []string
	blockedDestinations := map[[2]int]time.Time{}
	blockedFishingTargets := map[int]time.Time{}
	ultimaSincronizacaoAreas := time.Now()
	resyncsSemPeixe := 0
	tentativasMovimento := 0
	ultimoX, ultimoY := 0, 0
	ultimoEstadoFsh := ""
	avatarEmMovimento := false
	puxadaEnviada := false
	tentativasPuxada := 0
	var ultimaPuxada time.Time
	var movimento, lancamento time.Time
	roomCatalogPending := false
	var roomCatalogDeadline time.Time
	phase := fishingPhaseSeeking
	lastFishingStatusBytes := -1
	if coordinator == nil {
		coordinator = newFishingCoordinatorClient()
	}
	setPhase := func(next fishingPhase) {
		if phase == next {
			return
		}
		phase = next
		fmt.Printf("MARCO_OK: FISHING_PHASE fase=%s alvo=%d\n", phase, alvo)
	}
	clearTarget := func() {
		if alvo != 0 {
			coordinator.releaseTarget(roomKey, alvo)
		}
		alvo = 0
		lancamento = time.Time{}
		movimento = time.Time{}
		ultimoEstadoFsh = ""
		puxadaEnviada = false
		tentativasPuxada = 0
		ultimaPuxada = time.Time{}
		setPhase(fishingPhaseSeeking)
	}
	defer func() {
		clearTarget()
		coordinator.releaseRoom()
	}()
	for {
		if time.Now().After(nextStatsRefresh) {
			if err := requestFishingStats(conn, cryptoState); err != nil {
				return err
			}
			nextStatsRefresh = time.Now().Add(2 * time.Minute)
		}
		if roomCatalogPending && time.Now().After(roomCatalogDeadline) {
			fmt.Println("MARCO_OK: ROOM_CATALOG_ERROR erro=o navegador não respondeu a tempo")
			roomCatalogPending = false
		}
		select {
		case command, ok := <-commands:
			if !ok {
				return fmt.Errorf("canal de controle encerrado")
			}
			if handled, err := handleGlobalChatCommand(conn, cryptoState, command); handled {
				if err != nil {
					return err
				}
				continue
			}
			if isAutomationStop(command) {
				if err := quitRoomImmediately(conn, cryptoState); err != nil {
					return err
				}
				fmt.Println("MARCO_OK: FISHING_STOPPED")
				fmt.Println("MARCO_OK: AUTOMATION_STOPPED")
				return nil
			}
			if command == "rooms:refresh" {
				if err := refreshBusyRooms(conn, cryptoState, reader); err != nil {
					fmt.Printf("MARCO_OK: ROOM_CATALOG_ERROR erro=%s\n", err)
				}
			} else if owner, ok := parseRoomOwnerSearchCommand(command); ok {
				if err := searchRoomsByOwner(conn, cryptoState, reader, owner); err != nil {
					fmt.Printf("MARCO_OK: ROOM_CATALOG_ERROR erro=%s\n", err)
				}
			}
		default:
		}
		if len(areas) == 0 && time.Since(ultimaSincronizacaoAreas) > 15*time.Second {
			if err := sendEncrypted(conn, cryptoState, getObjects); err != nil {
				return err
			}
			ultimaSincronizacaoAreas = time.Now()
			if positionKnown && len(heightmap) > 0 {
				resyncsSemPeixe++
			}
			fmt.Printf("MARCO_OK: RESYNC_PEIXES solicitando objetos da sala tentativa=%d\n", resyncsSemPeixe)
			if shouldRecoverFishingRoom(positionKnown, heightmap, resyncsSemPeixe) {
				fmt.Printf("MARCO_OK: FISHING_ROOM_STALE resyncs=%d\n", resyncsSemPeixe)
				return errFishingRoomStale
			}
		}
		// O V72 estável escolhe um peixe e permanece decidido nele. A versão
		// anterior do headless recalculava o "melhor" alvo a cada pacote de
		// posição, cancelando a caminhada e alternando entre vários peixes.
		// Só liberamos este alvo quando ele some, a captura termina ou ocorre um
		// timeout real de pesca.
		if lancamento.IsZero() && positionKnown && len(heightmap) > 0 {
			if alvo != 0 {
				if _, existe := areas[alvo]; !existe {
					clearTarget()
				}
			}
			if alvo == 0 {
				for tentativas := 0; tentativas < len(areas) && alvo == 0; tentativas++ {
					candidatos := availableFishingAreas(areas, blockedFishingTargets, time.Now())
					candidato, encontrado := closestReachableFishingTarget(heightmap, blockedDestinations, meuX, meuY, candidatos)
					if !encontrado {
						break
					}
					if !coordinator.reserveTarget(roomKey, candidato.id) {
						blockedFishingTargets[candidato.id] = time.Now().Add(cooldownPeixeOcupado)
						fmt.Printf("MARCO_OK: FISHING_TARGET_BUSY id=%d por=%s\n", candidato.id, cooldownPeixeOcupado)
						continue
					}
					alvo, alvoX, alvoY = candidato.id, candidato.fishX, candidato.fishY
					destX, destY = candidato.destX, candidato.destY
					if candidato.path > 0 {
						movimento = time.Now()
						tentativasMovimento = 1
						ultimoX, ultimoY = meuX, meuY
						setPhase(fishingPhaseMoving)
						if err := sendEncrypted(conn, cryptoState, moveAvatar, origins.EncodeVL64(destX), origins.EncodeVL64(destY)); err != nil {
							return err
						}
						fmt.Printf("MARCO_OK: ALVO_TRAVADO_MOVENDO id=%d destino=%d,%d peixe=%d,%d distância=%.2f caminho=%d\n", alvo, destX, destY, alvoX, alvoY, candidato.distance, candidato.path)
					} else {
						movimento = time.Time{}
						fmt.Printf("MARCO_OK: ALVO_TRAVADO_IMEDIATO id=%d peixe=%d,%d distância=%.2f\n", alvo, alvoX, alvoY, candidato.distance)
					}
				}
			}
		}
		if podeLancar(alvo, lancamento, positionKnown, avatarEmMovimento, meuX, meuY, alvoX, alvoY) {
			if _, existe := areas[alvo]; !existe {
				clearTarget()
				continue
			}
			// Na sessão headless, o lançamento inicial precisa ser duplicado para
			// armar o minijogo sem o cliente gráfico. As capturas reais anteriores
			// usaram dois STARTFISHING aqui; a cadência da puxada é independente.
			if err := sendEncrypted(conn, cryptoState, startFishing, origins.EncodeVL64(alvo)); err != nil {
				return err
			}
			if err := sendEncrypted(conn, cryptoState, startFishing, origins.EncodeVL64(alvo)); err != nil {
				return err
			}
			lancamento = time.Now()
			movimento = time.Time{}
			ultimoEstadoFsh = ""
			puxadaEnviada = false
			tentativasPuxada = 0
			ultimaPuxada = time.Time{}
			setPhase(fishingPhaseCastSent)
			fmt.Printf("MARCO_OK: CAST_SENT posição=%d,%d alvo=%d,%d\n", meuX, meuY, alvoX, alvoY)
		}
		if alvo != 0 && lancamento.IsZero() && !movimento.IsZero() && time.Since(movimento) > intervaloRevisaoMovimento {
			if distancia(meuX, meuY, alvoX, alvoY) <= alcancePesca {
				movimento = time.Time{}
				continue
			}
			// Um STATUS /mv confirma que o servidor ainda está processando a
			// caminhada. Reenviar ou trocar o destino nessa fase criava rotas
			// bloqueadas artificiais e fazia a conta parecer indecisa.
			if avatarEmMovimento {
				movimento = time.Now()
				continue
			}
			if meuX != ultimoX || meuY != ultimoY {
				ultimoX, ultimoY = meuX, meuY
				tentativasMovimento = 1
			} else {
				tentativasMovimento++
			}
			if shouldRetryFishingMovement(avatarEmMovimento, tentativasMovimento) {
				anteriorX, anteriorY := destX, destY
				blockedDestinations[[2]int{destX, destY}] = time.Now().Add(8 * time.Second)
				novoX, novoY, caminho, ok := fishingDestination(heightmap, blockedDestinations, meuX, meuY, alvoX, alvoY)
				if ok {
					destX, destY = novoX, novoY
					tentativasMovimento = 1
					fmt.Printf("MARCO_OK: DESTINO_ALTERNATIVO alvo=%d anterior=%d,%d novo=%d,%d caminho=%d\n", alvo, anteriorX, anteriorY, destX, destY, caminho)
				} else {
					// O peixe continua travado, mas aguardamos o mapa/desbloqueio em vez
					// de inundar o servidor ou alternar aleatoriamente de alvo.
					movimento = time.Now()
					fmt.Printf("MARCO_OK: DESTINO_TEMPORARIAMENTE_INACESSIVEL alvo=%d atual=%d,%d\n", alvo, meuX, meuY)
					continue
				}
			}
			// Durante progresso normal, reforça sempre a mesma coordenada final.
			if err := sendEncrypted(conn, cryptoState, moveAvatar, origins.EncodeVL64(destX), origins.EncodeVL64(destY)); err != nil {
				return err
			}
			movimento = time.Now()
		}
		// O driver estável do G-Earth envia uma única puxada na transição de
		// mordida. Reforços sucessivos aumentavam a janela sem resultado e
		// prendiam a conta no mesmo peixe por tempo demais.
		if puxadaEnviada && tentativasPuxada < maximoReforcosPuxada && time.Since(ultimaPuxada) > intervaloReforcoPuxada {
			if err := sendEncrypted(conn, cryptoState, startFishing, origins.EncodeVL64(alvo)); err != nil {
				return err
			}
			tentativasPuxada++
			ultimaPuxada = time.Now()
			fmt.Printf("MARCO_OK: PUXADA_REFORCADA alvo=%d tentativa=%d\n", alvo, tentativasPuxada)
			if tentativasPuxada >= maximoReforcosPuxada {
				fmt.Printf("MARCO_OK: JANELA_PUXADA_CONCLUIDA alvo=%d\n", alvo)
			}
		}

		// Recupera lançamentos ignorados e uma puxada cujo resultado se perdeu.
		// O alvo permanece travado até FISHING_CHAT/END_FISHING ou até este
		// timeout explícito; assim não há troca no meio de uma captura válida.
		limitePesca := fishingTimeoutForPhase(phase)
		if !lancamento.IsZero() && time.Since(lancamento) > limitePesca {
			if alvo != 0 {
				cooldown := fishingTimeoutCooldown(phase)
				blockedFishingTargets[alvo] = time.Now().Add(cooldown)
				fmt.Printf("MARCO_OK: ALVO_SEM_RESPOSTA_BLOQUEADO id=%d por=%s fase=%s\n", alvo, cooldown, phase)
			}
			fmt.Printf("MARCO_OK: FISHING_TIMEOUT fase=%s nova tentativa\n", phase)
			clearTarget()
		}
		// Mesmo ciclo rápido de decisão usado pelo bot do G-Earth: reduz o
		// intervalo máximo entre surgir/sumir um peixe e a próxima ação.
		_ = conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
		packet, err := reader.Next()
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			return err
		}
		header, err := origins.Header(packet)
		if err != nil {
			return err
		}
		if header == recommendedRoomList && roomCatalogPending {
			if err := emitRecommendedRoomCatalog(packet[2:]); err != nil {
				fmt.Printf("MARCO_OK: ROOM_CATALOG_ERROR erro=%s\n", err)
			}
			roomCatalogPending = false
			continue
		}
		if header == ping {
			if err := replyPong(conn, cryptoState, packet); err != nil {
				return err
			}
			continue
		}
		if os.Getenv("HABBO_TRACE_PACKETS") == "1" {
			fmt.Printf("TRACE_ROOM header=%d bytes=%d\n", header, len(packet)-2)
		}
		switch header {
		case fishingStats:
			if err := emitFishingStats(packet[2:]); err != nil {
				fmt.Printf("MARCO_OK: FISHING_STATS_ERROR erro=%s\n", err)
			}
		case usersInRoom:
			usersPackets++
			entities, e := parseRoomUsers(packet[2:])
			if e != nil {
				log.Printf("USERS ignorado: %v", e)
				continue
			}
			for _, entity := range entities {
				fmt.Printf("MARCO_OK: ROOM_USER pacote=%d index=%d nome=%s x=%d y=%d tipo=%d\n", usersPackets, entity.index, entity.name, entity.x, entity.y, entity.kind)
				if entity.kind == 1 {
					entityNames[entity.index] = entity.name
				}
				if accountName != "" && entity.kind == 1 && strings.EqualFold(entity.name, accountName) {
					ownIndex = entity.index
					meuX, meuY = entity.x, entity.y
					positionKnown = true
					if !doorKnown {
						doorX, doorY, doorKnown = meuX, meuY, true
						fmt.Printf("MARCO_OK: ROOM_DOOR_MAPPED x=%d y=%d\n", doorX, doorY)
					}
					fmt.Printf("MARCO_OK: OWN_INDEX_BY_NAME index=%d nome=%s x=%d y=%d\n", ownIndex, entity.name, meuX, meuY)
				}
				if entity.index == ownIndex && entity.kind == 1 && entity.name != identifiedAccount {
					identifiedAccount = entity.name
					fmt.Printf("MARCO_OK: ACCOUNT_IDENTIFIED nome=%s\n", entity.name)
				}
			}
			if shouldUseSingleUserFallback(accountName, ownIndex, entities) {
				ownIndex = entities[0].index
				meuX, meuY = entities[0].x, entities[0].y
				positionKnown = true
				if !doorKnown {
					doorX, doorY, doorKnown = meuX, meuY, true
				}
				fmt.Printf("MARCO_OK: OWN_INDEX_FROM_USERS index=%d nome=%s x=%d y=%d\n", ownIndex, entities[0].name, meuX, meuY)
				if entities[0].name != identifiedAccount {
					identifiedAccount = entities[0].name
					fmt.Printf("MARCO_OK: ACCOUNT_IDENTIFIED nome=%s\n", entities[0].name)
				}
			}
		case floorHeightmap:
			// No protocolo Shockwave/Origins o HEIGHTMAP ocupa todo o payload e,
			// diferente das strings comuns, não termina com o byte 0x02.
			raw := strings.TrimRight(string(packet[2:]), "\x00\x02")
			heightmap = strings.Split(raw, "\r")
			fmt.Printf("MARCO_OK: HEIGHTMAP_READY linhas=%d\n", len(heightmap))
		case activeObjects:
			list, e := allFishingAreas(packet[2:])
			if e != nil {
				log.Printf("pacote ACTIVEOBJECTS ignorado: %v", e)
				continue
			}
			for _, a := range list {
				areas[a[0]] = fishingArea{a[0], a[1], a[2]}
			}
			if len(areas) > 0 {
				resyncsSemPeixe = 0
			}
			ultimaSincronizacaoAreas = time.Now()
			fmt.Printf("MARCO_OK: PEIXES_SINCRONIZADOS recebidos=%d monitorados=%d\n", len(list), len(areas))
		case activeObjectAdd:
			id, x, y, ok, e := addedFishingArea(packet[2:])
			if e != nil {
				log.Printf("pacote ACTIVEOBJECT_ADD ignorado: %v", e)
				continue
			}
			if ok {
				areas[id] = fishingArea{id, x, y}
			}
		case activeObjectRemove:
			idText := strings.TrimRight(string(packet[2:]), "\x00\x02")
			id, _ := strconv.Atoi(idText)
			delete(areas, id)
			if id == alvo {
				if shouldReleaseRemovedTarget(lancamento) {
					clearTarget()
					fmt.Println("MARCO_OK: ALVO_SUMIU_ANTES_DO_LANCAMENTO procurando próximo peixe")
				} else {
					// O objeto some visualmente antes de o servidor concluir o
					// minijogo. Não libera movimento nem troca de alvo aqui.
					fmt.Println("MARCO_OK: ALVO_REMOVIDO_AGUARDANDO_RESULTADO pesca permanece travada")
				}
			}
		case statusUpdate:
			statuses, e := parseEntityStatuses(packet[2:])
			if e != nil {
				log.Printf("STATUS ignorado: %v", e)
				continue
			}
			for _, s := range statuses {
				if ownIndex < 0 && !movimento.IsZero() && strings.Contains(s.action, "mv") {
					ownIndex = s.index
					fmt.Printf("MARCO_OK: OWN_INDEX index=%d ação=%s\n", ownIndex, s.action)
					if name := entityNames[ownIndex]; name != "" && name != identifiedAccount {
						identifiedAccount = name
						fmt.Printf("MARCO_OK: ACCOUNT_IDENTIFIED nome=%s\n", name)
					}
				}
				if s.index == ownIndex {
					meuX, meuY = s.x, s.y
					positionKnown = true
					avatarEmMovimento = strings.Contains(s.action, "/mv ") || strings.HasPrefix(s.action, "mv ")
					if strings.Contains(s.action, "fsh ") && os.Getenv("HABBO_FISHING_VERBOSE") == "1" {
						fmt.Printf("TRACE_FISHING posição=%d,%d ação=%s\n", meuX, meuY, s.action)
					}
					if estado, ok := estadoPesca(s.action, alvoX, alvoY); ok && alvo != 0 && !lancamento.IsZero() {
						if ultimoEstadoFsh == "" {
							ultimoEstadoFsh = estado
							lancamento = time.Now()
							setPhase(fishingPhaseWaitingBite)
							fmt.Printf("MARCO_OK: FISHING_STATE estado=%s alvo=%d\n", estado, alvo)
						} else if estado != ultimoEstadoFsh {
							anterior := ultimoEstadoFsh
							ultimoEstadoFsh = estado
							lancamento = time.Now()
							if devePuxar(anterior, estado, puxadaEnviada) {
								puxadaEnviada = true
								tentativasPuxada = 1
								ultimaPuxada = time.Now()
								if err := sendEncrypted(conn, cryptoState, startFishing, origins.EncodeVL64(alvo)); err != nil {
									return err
								}
								setPhase(fishingPhasePullSent)
								fmt.Printf("MARCO_OK: MORDIDA_PUXADA estado=%s->%s alvo=%d\n", anterior, estado, alvo)
							} else if rodadaDePescaReiniciada(anterior, estado, puxadaEnviada) {
								fmt.Printf("MARCO_OK: RODADA_REINICIADA alvo=%d aguardando END_FISHING\n", alvo)
							}
						}
					}
				}
			}
		case startFishingAck:
			setPhase(fishingPhaseWaitingBite)
			fmt.Println("MARCO_OK: FISHING_CONFIRMED servidor aceitou o lançamento")
		case fishingStatus:
			if packetBytes := len(packet) - 2; packetBytes != lastFishingStatusBytes {
				lastFishingStatusBytes = packetBytes
				fmt.Printf("MARCO_OK: FISHING_STATUS_OBSERVADO fase=%s bytes=%d\n", phase, packetBytes)
			}
		case fishingChat:
			msg := strings.TrimRight(string(packet[2:]), "\x00\x02")
			fmt.Printf("MARCO_OK: FISHING_CHAT %s\n", msg)
			if strings.Contains(strings.ToUpper(msg), "EXP") {
				// A mensagem de EXP é a confirmação definitiva da captura. Em
				// algumas salas o END_FISHING não vem logo depois; esperar por ele
				// transforma uma captura válida em timeout e relança no mesmo peixe.
				clearTarget()
				fmt.Println("MARCO_OK: CAPTURA_CONFIRMADA procurando próximo peixe")
			}
		case endFishing:
			clearTarget()
			fmt.Println("MARCO_OK: END_FISHING procurando próximo peixe")
		default:
			continue
		}
		if err != nil {
			return err
		}
	}
}

func nextWalkStep(heightmap []string, blockedUntil map[[2]int]time.Time, fromX, fromY, toX, toY int) (int, int, bool) {
	type point struct{ x, y int }
	start := point{fromX, fromY}
	goal := point{toX, toY}
	queue := []point{start}
	seen := map[point]bool{start: true}
	parent := map[point]point{}
	dirs := []point{{1, 0}, {-1, 0}, {0, 1}, {0, -1}, {1, 1}, {1, -1}, {-1, 1}, {-1, -1}}
	found := false
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur == goal {
			found = true
			break
		}
		for _, d := range dirs {
			n := point{cur.x + d.x, cur.y + d.y}
			if seen[n] || tileTemporariamenteBloqueado(blockedUntil, [2]int{n.x, n.y}) || n.y < 0 || n.y >= len(heightmap) || n.x < 0 || n.x >= len(heightmap[n.y]) {
				continue
			}
			tile := heightmap[n.y][n.x]
			if tile == 'x' || tile == 'X' {
				continue
			}
			seen[n] = true
			parent[n] = cur
			queue = append(queue, n)
		}
	}
	if !found {
		return fromX, fromY, false
	}
	step := goal
	for parent[step] != start && step != start {
		step = parent[step]
	}
	return step.x, step.y, true
}

func bestFishingTile(heightmap []string, blockedUntil map[[2]int]time.Time, fromX, fromY, fishX, fishY int) (int, int, int, bool) {
	type point struct{ x, y, d int }
	queue := []point{{fromX, fromY, 0}}
	seen := map[[2]int]bool{{fromX, fromY}: true}
	dirs := [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}, {1, 1}, {1, -1}, {-1, 1}, {-1, -1}}
	bestDist := int(^uint(0) >> 1)
	bx, by := 0, 0
	found := false
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		fishDistance := distancia(current.x, current.y, fishX, fishY)
		if current.d < bestDist && fishDistance > 0 && fishDistance <= alcancePesca {
			bestDist = current.d
			bx = current.x
			by = current.y
			found = true
		}
		for _, dir := range dirs {
			nx, ny := current.x+dir[0], current.y+dir[1]
			key := [2]int{nx, ny}
			if seen[key] || tileTemporariamenteBloqueado(blockedUntil, key) || ny < 0 || ny >= len(heightmap) || nx < 0 || nx >= len(heightmap[ny]) {
				continue
			}
			tile := heightmap[ny][nx]
			if tile == 'x' || tile == 'X' {
				continue
			}
			seen[key] = true
			queue = append(queue, point{nx, ny, current.d + 1})
		}
	}
	return bx, by, bestDist, found
}

func tileTemporariamenteBloqueado(blockedUntil map[[2]int]time.Time, tile [2]int) bool {
	until, exists := blockedUntil[tile]
	if !exists {
		return false
	}
	if time.Now().Before(until) {
		return true
	}
	delete(blockedUntil, tile)
	return false
}

// Replica o fallback do bot G-Earth: deixa o servidor decidir a rota até o
// piso caminhável mais próximo que já fique dentro do alcance da vara.
func directFishingTile(heightmap []string, fromX, fromY, fishX, fishY int) (int, int, bool) {
	bestX, bestY := 0, 0
	bestDistance := math.MaxFloat64
	found := false
	for y, row := range heightmap {
		for x := range row {
			if row[x] == 'x' || row[x] == 'X' || distancia(x, y, fishX, fishY) <= 0 || distancia(x, y, fishX, fishY) > alcancePesca {
				continue
			}
			d := distancia(fromX, fromY, x, y)
			if d < bestDistance {
				bestX, bestY, bestDistance, found = x, y, d, true
			}
		}
	}
	return bestX, bestY, found
}

type roomUser struct {
	index, id, x, y, kind int
	name                  string
}

func shouldUseSingleUserFallback(accountName string, ownIndex int, entities []roomUser) bool {
	return accountName == "" && ownIndex < 0 && len(entities) == 1 && entities[0].kind == 1
}

func parseRoomUsers(payload []byte) ([]roomUser, error) {
	c := &packetCursor{data: payload}
	count, err := c.integer()
	if err != nil {
		return nil, err
	}
	out := make([]roomUser, 0, count)
	for i := 0; i < count; i++ {
		idx, e := c.integer()
		if e != nil {
			return nil, e
		}
		id, e := c.integer()
		if e != nil {
			return nil, e
		}
		name, e := c.str()
		if e != nil {
			return nil, e
		}
		for n := 0; n < 3; n++ {
			if _, e = c.str(); e != nil {
				return nil, e
			}
		}
		x, e := c.integer()
		if e != nil {
			return nil, e
		}
		y, e := c.integer()
		if e != nil {
			return nil, e
		}
		if _, e = c.str(); e != nil {
			return nil, e
		}
		if _, e = c.str(); e != nil {
			return nil, e
		}
		if _, e = c.str(); e != nil {
			return nil, e
		}
		kind, e := c.integer()
		if e != nil {
			return nil, e
		}
		switch kind {
		case 1:
			for n := 0; n < 2; n++ {
				if _, e = c.str(); e != nil {
					return nil, e
				}
			}
			for n := 0; n < 3; n++ {
				if _, e = c.integer(); e != nil {
					return nil, e
				}
			}
		case 3, 4:
			if _, e = c.integer(); e != nil {
				return nil, e
			}
		}
		out = append(out, roomUser{idx, id, x, y, kind, name})
	}
	return out, nil
}

func allFishingAreas(payload []byte) ([][3]int, error) {
	c := &packetCursor{data: payload}
	count, err := c.integer()
	if err != nil {
		return nil, err
	}
	out := [][3]int{}
	for i := 0; i < count; i++ {
		id, x, y, class, e := readActiveObject(c)
		if e != nil {
			return nil, e
		}
		if strings.Contains(strings.ToLower(class), "fish_area") {
			out = append(out, [3]int{id, x, y})
		}
	}
	return out, nil
}

type entityStatus struct {
	index, x, y int
	action      string
}

func parseEntityStatuses(payload []byte) ([]entityStatus, error) {
	c := &packetCursor{data: payload}
	count, err := c.integer()
	if err != nil {
		return nil, err
	}
	out := make([]entityStatus, 0, count)
	for i := 0; i < count; i++ {
		idx, e := c.integer()
		if e != nil {
			return nil, e
		}
		x, e := c.integer()
		if e != nil {
			return nil, e
		}
		y, e := c.integer()
		if e != nil {
			return nil, e
		}
		if _, e = c.str(); e != nil {
			return nil, e
		}
		if _, e = c.integer(); e != nil {
			return nil, e
		}
		if _, e = c.integer(); e != nil {
			return nil, e
		}
		action, e := c.str()
		if e != nil {
			return nil, e
		}
		out = append(out, entityStatus{idx, x, y, action})
	}
	return out, nil
}
func distancia(x1, y1, x2, y2 int) float64 { return math.Hypot(float64(x2-x1), float64(y2-y1)) }

type fishingArea struct{ id, x, y int }

type fishingTarget struct {
	id, fishX, fishY int
	destX, destY     int
	path             int
	distance         float64
}

// closestReachableFishingTarget escolhe somente um peixe que tenha um piso
// acessível dentro do alcance da vara. A prioridade é o número real de casas
// caminhadas; a distância física só desempata caminhos equivalentes. Assim um
// peixe aparentemente próximo atrás de um obstáculo não vence uma rota curta.
func closestReachableFishingTarget(heightmap []string, blockedUntil map[[2]int]time.Time, fromX, fromY int, areas map[int]fishingArea) (fishingTarget, bool) {
	best := fishingTarget{}
	found := false
	for id, area := range areas {
		destX, destY, path, ok := fishingDestination(heightmap, blockedUntil, fromX, fromY, area.x, area.y)
		if !ok {
			continue
		}
		distance := distancia(fromX, fromY, area.x, area.y)
		if !found || betterFishCandidate(distance, path, id, best.distance, best.path, best.id) {
			best = fishingTarget{
				id:       id,
				fishX:    area.x,
				fishY:    area.y,
				destX:    destX,
				destY:    destY,
				path:     path,
				distance: distance,
			}
			found = true
		}
	}
	return best, found
}

// availableFishingAreas remove somente os alvos que esta própria sessão
// acabou de testar sem receber sequer o início do minijogo. O bloqueio é
// local e curto: não altera o mapa nem interfere nas demais contas.
func availableFishingAreas(areas map[int]fishingArea, unavailableUntil map[int]time.Time, now time.Time) map[int]fishingArea {
	available := make(map[int]fishingArea, len(areas))
	for id, area := range areas {
		if until, unavailable := unavailableUntil[id]; unavailable {
			if now.Before(until) {
				continue
			}
			delete(unavailableUntil, id)
		}
		available[id] = area
	}
	return available
}

func betterFishCandidate(distance float64, path, id int, bestDistance float64, bestPath, bestID int) bool {
	return path < bestPath ||
		(path == bestPath && distance < bestDistance) ||
		(path == bestPath && distance == bestDistance && (bestID == 0 || id < bestID))
}

func shouldRetryFishingMovement(avatarMoving bool, attempts int) bool {
	return !avatarMoving && attempts >= tentativasAntesDesvio
}

// shouldRecoverFishingRoom só ativa a reentrada quando a sessão já recebeu o
// mapa e a própria posição, mas passou por várias consultas de objetos vazias.
// Isso separa o carregamento inicial normal de uma sessão que saiu da sala sem
// encerrar o socket.
func shouldRecoverFishingRoom(positionKnown bool, heightmap []string, emptyResyncs int) bool {
	return positionKnown && len(heightmap) > 0 && emptyResyncs >= maxResyncsSemPeixe
}

func fishingDestination(heightmap []string, blockedUntil map[[2]int]time.Time, meuX, meuY, alvoX, alvoY int) (int, int, int, bool) {
	if len(heightmap) == 0 {
		return 0, 0, 0, false
	}
	return bestFishingTile(heightmap, blockedUntil, meuX, meuY, alvoX, alvoY)
}

// legacyFishingDestination replica moverParaAlcance do Bot-Pesca V72. O
// servidor recebe um único destino próximo ao peixe e calcula a caminhada;
// não fragmentamos a rota em passos nem a trocamos durante o percurso.
func legacyFishingDestination(meuX, meuY, alvoX, alvoY int) (int, int) {
	destX, destY := meuX, meuY
	difX, difY := alvoX-meuX, alvoY-meuY
	if math.Abs(float64(difX)) > 2 {
		if difX > 0 {
			destX = alvoX - 2
		} else {
			destX = alvoX + 2
		}
	} else {
		destX = alvoX
	}
	if math.Abs(float64(difY)) > 2 {
		if difY > 0 {
			destY = alvoY - 2
		} else {
			destY = alvoY + 2
		}
	} else {
		destY = alvoY
	}
	return destX, destY
}

func podeLancar(alvo int, lancamento time.Time, positionKnown, _ bool, meuX, meuY, alvoX, alvoY int) bool {
	// O V72 lança assim que entra no alcance, mesmo que o último STATUS ainda
	// contenha /mv. Esperar um pacote explícito de parada deixava o headless
	// parado ao lado do peixe e o MOVE para o próprio pé criava o zigue-zague.
	return alvo != 0 && lancamento.IsZero() && positionKnown && distancia(meuX, meuY, alvoX, alvoY) <= alcancePesca
}

func shouldReleaseRemovedTarget(castStarted time.Time) bool {
	return castStarted.IsZero()
}

func estadoPesca(action string, alvoX, alvoY int) (string, bool) {
	for _, trecho := range strings.Split(action, "/") {
		trecho = strings.TrimSpace(trecho)
		if !strings.HasPrefix(trecho, "fsh ") {
			continue
		}
		partes := strings.Split(strings.TrimPrefix(trecho, "fsh "), ",")
		if len(partes) < 4 {
			continue
		}
		x, errX := strconv.Atoi(partes[0])
		y, errY := strconv.Atoi(partes[1])
		if errX != nil || errY != nil || x != alvoX || y != alvoY {
			continue
		}
		return partes[3], true
	}
	return "", false
}

func devePuxar(anterior, atual string, puxadaPendente bool) bool {
	return !puxadaPendente && anterior == "0" && atual != "0"
}

func rodadaDePescaReiniciada(anterior, atual string, puxadaPendente bool) bool {
	return puxadaPendente && anterior != "0" && atual == "0"
}

func firstFishingArea(payload []byte) (int, int, int, bool, error) {
	c := &packetCursor{data: payload}
	count, err := c.integer()
	if err != nil {
		return 0, 0, 0, false, err
	}
	for i := 0; i < count; i++ {
		id, x, y, class, err := readActiveObject(c)
		if err != nil {
			return 0, 0, 0, false, err
		}
		if strings.Contains(strings.ToLower(class), "fish_area") {
			return id, x, y, true, nil
		}
	}
	return 0, 0, 0, false, nil
}

func addedFishingArea(payload []byte) (int, int, int, bool, error) {
	id, x, y, class, err := readActiveObject(&packetCursor{data: payload})
	if err != nil {
		return 0, 0, 0, false, err
	}
	return id, x, y, strings.Contains(strings.ToLower(class), "fish_area"), nil
}

func readActiveObject(c *packetCursor) (int, int, int, string, error) {
	idText, err := c.str()
	if err != nil {
		return 0, 0, 0, "", err
	}
	if _, err = c.integer(); err != nil {
		return 0, 0, 0, "", err
	}
	class, err := c.str()
	if err != nil {
		return 0, 0, 0, "", err
	}
	x, err := c.integer()
	if err != nil {
		return 0, 0, 0, "", err
	}
	y, err := c.integer()
	if err != nil {
		return 0, 0, 0, "", err
	}
	for n := 0; n < 3; n++ {
		if _, err = c.integer(); err != nil {
			return 0, 0, 0, "", err
		}
	}
	for n := 0; n < 3; n++ {
		if _, err = c.str(); err != nil {
			return 0, 0, 0, "", err
		}
	}
	if _, err = c.integer(); err != nil {
		return 0, 0, 0, "", err
	}
	if _, err = c.str(); err != nil {
		return 0, 0, 0, "", err
	}
	handler, err := c.integer()
	if err != nil {
		return 0, 0, 0, "", err
	}
	switch handler {
	case 1:
		for n := 0; n < 3; n++ {
			if _, err = c.integer(); err != nil {
				return 0, 0, 0, "", err
			}
		}
	case 2:
		if _, err = c.str(); err != nil {
			return 0, 0, 0, "", err
		}
		products, e := c.integer()
		if e != nil {
			return 0, 0, 0, "", e
		}
		for n := 0; n < products; n++ {
			if _, err = c.str(); err != nil {
				return 0, 0, 0, "", err
			}
			if _, err = c.str(); err != nil {
				return 0, 0, 0, "", err
			}
		}
	case 3:
		if _, err = c.integer(); err != nil {
			return 0, 0, 0, "", err
		}
	}
	for n := 0; n < 2; n++ {
		if _, err = c.integer(); err != nil {
			return 0, 0, 0, "", err
		}
	}
	var id int
	if _, err := fmt.Sscanf(idText, "%d", &id); err != nil {
		return 0, 0, 0, "", err
	}
	return id, x, y, class, nil
}

type packetCursor struct {
	data   []byte
	offset int
}

func (c *packetCursor) integer() (int, error) {
	v, n, err := origins.DecodeVL64(c.data[c.offset:])
	if err != nil {
		return 0, err
	}
	c.offset += n
	return v, nil
}
func (c *packetCursor) str() (string, error) {
	if c.offset >= len(c.data) {
		return "", fmt.Errorf("string truncada")
	}
	i := c.offset
	for i < len(c.data) && c.data[i] != 2 {
		i++
	}
	if i >= len(c.data) {
		return "", fmt.Errorf("terminador de string ausente")
	}
	v := string(c.data[c.offset:i])
	c.offset = i + 1
	return v, nil
}

func parseNavigatorNodes(payload []byte) ([]navigatorNode, error) {
	c := &packetCursor{data: payload}
	if _, err := c.integer(); err != nil {
		return nil, err
	} // node mask
	nodes := []navigatorNode{}
	for c.offset < len(c.data) {
		id, err := c.integer()
		if err != nil {
			return nil, err
		}
		typ, err := c.integer()
		if err != nil {
			return nil, err
		}
		name, err := c.str()
		if err != nil {
			return nil, err
		}
		users, err := c.integer()
		if err != nil { // usuários
			return nil, err
		}
		capacity, err := c.integer()
		if err != nil { // capacidade
			return nil, err
		}
		parentID, err := c.integer()
		if err != nil {
			return nil, err
		}
		node := navigatorNode{ID: id, Type: typ, ParentID: parentID, Name: name}
		if typ == 1 || typ == 3 {
			if _, err := c.str(); err != nil {
				return nil, err
			}
			port, err := c.integer()
			if err != nil {
				return nil, err
			}
			door, err := c.integer()
			if err != nil {
				return nil, err
			}
			if _, err := c.str(); err != nil {
				return nil, err
			}
			if _, err := c.integer(); err != nil {
				return nil, err
			}
			if _, err := c.integer(); err != nil {
				return nil, err
			}
			node.Room = &publicRoom{ID: id, Name: name, Users: users, Capacity: capacity, Access: "open", Port: port, Door: door}
			if typ == 3 {
				if _, err := c.integer(); err != nil {
					return nil, err
				}
			}
		} else if typ == 2 {
			count, err := c.integer()
			if err != nil {
				return nil, err
			}
			node.Type = 0 // categorias de flats são navegáveis, como no cliente oficial.
			node.Rooms = make([]publicRoom, 0, count)
			for i := 0; i < count; i++ {
				roomID, err := c.integer()
				if err != nil {
					return nil, err
				}
				roomName, err := c.str()
				if err != nil {
					return nil, err
				}
				owner, err := c.str()
				if err != nil {
					return nil, err
				}
				access, err := c.str()
				if err != nil {
					return nil, err
				}
				roomUsers, err := c.integer()
				if err != nil {
					return nil, err
				}
				roomCapacity, err := c.integer()
				if err != nil {
					return nil, err
				}
				description, err := c.str()
				if err != nil {
					return nil, err
				}
				node.Rooms = append(node.Rooms, publicRoom{ID: roomID, Name: roomName, Owner: owner, Access: access, Users: roomUsers, Capacity: roomCapacity, Description: description})
			}
		} else if typ != 0 {
			return nil, fmt.Errorf("tipo de nó do navegador desconhecido: %d", typ)
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

func normalizeRoomName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	replacer := strings.NewReplacer(
		"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a",
		"é", "e", "è", "e", "ê", "e", "ë", "e",
		"í", "i", "ì", "i", "î", "i", "ï", "i",
		"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o",
		"ú", "u", "ù", "u", "û", "u", "ü", "u", "ç", "c",
	)
	return strings.Join(strings.Fields(replacer.Replace(value)), " ")
}

func matchesFishingRoom(name string, config fishingRoomConfig) bool {
	normalized := normalizeRoomName(name)
	for _, alias := range config.Aliases {
		if strings.Contains(normalized, normalizeRoomName(alias)) {
			return true
		}
	}
	return false
}

func keepAlive(conn net.Conn, cryptoState *origins.Crypto, reader *encryptedPacketReader) error {
	// A autorização pode levar mais de vinte segundos e a sessão deve permanecer
	// conectada indefinidamente depois do LOGIN_OK.
	_ = conn.SetDeadline(time.Time{})
	for {
		packet, err := reader.Next()
		if err != nil {
			return err
		}
		header, err := origins.Header(packet)
		if err != nil {
			return err
		}
		if header == ping {
			if err := replyPong(conn, cryptoState, packet); err != nil {
				return err
			}
		}
	}
}

func awaitOpenIDLink(conn net.Conn, cryptoState *origins.Crypto, reader *encryptedPacketReader) (string, error) {
	for {
		packet, err := reader.Next()
		if err != nil {
			return "", err
		}
		header, err := origins.Header(packet)
		if err != nil {
			return "", err
		}
		log.Printf("recebido criptografado header=%d bytes=%d", header, len(packet)-2)
		switch header {
		case ping:
			if err := replyPong(conn, cryptoState, packet); err != nil {
				return "", err
			}
		case steamOpenIDLink:
			link, err := origins.IncomingString(packet, 2)
			if err != nil {
				return "", err
			}
			return link, nil
		}
	}
}

func requestOwnProfile(conn net.Conn, cryptoState *origins.Crypto, reader *encryptedPacketReader) string {
	if err := sendEncrypted(conn, cryptoState, infoRetrieve); err != nil {
		return ""
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	defer conn.SetReadDeadline(time.Time{})
	for {
		packet, err := reader.Next()
		if err != nil {
			return ""
		}
		header, err := origins.Header(packet)
		if err != nil {
			continue
		}
		if header == ping {
			_ = replyPong(conn, cryptoState, packet)
			continue
		}
		if header == userObject {
			name, err := parseOwnProfileName(packet[2:])
			if err == nil {
				return name
			}
		}
	}
}

func parseOwnProfileName(payload []byte) (string, error) {
	cursor := &packetCursor{data: payload}
	if _, err := cursor.integer(); err != nil {
		return "", err
	}
	return cursor.str()
}

func awaitLoginOK(conn net.Conn, cryptoState *origins.Crypto, reader *encryptedPacketReader, credentials loginConfig) error {
	passwordLogin := credentials.Email != ""
	currentLoginTried := false
	if passwordLogin {
		// Pings mantêm o socket vivo, portanto um ReadDeadline é necessário para
		// impedir que uma resposta desconhecida deixe o painel autenticando para
		// sempre. O fluxo Steam permanece sem esse limite porque depende do usuário.
		_ = conn.SetReadDeadline(time.Now().Add(45 * time.Second))
		defer conn.SetReadDeadline(time.Time{})
	}
	for {
		packet, err := reader.Next()
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				return fmt.Errorf("ERRO_LOGIN: autenticação não foi concluída pelo servidor em 45 segundos")
			}
			return fmt.Errorf("ERRO_LOGIN: login recusado; confira login, senha e verificação em duas etapas (%w)", err)
		}
		header, err := origins.Header(packet)
		if err != nil {
			return err
		}
		log.Printf("recebido criptografado header=%d bytes=%d", header, len(packet)-2)
		fmt.Printf("TRACE_LOGIN header=%d bytes=%d\n", header, len(packet)-2)
		switch header {
		case ping:
			if err := replyPong(conn, cryptoState, packet); err != nil {
				return err
			}
		case rights:
			// RIGHTS normalmente antecede LOGIN_OK.
		case loginOK:
			return nil
		case noLoginPermission:
			if passwordLogin && !currentLoginTried {
				// Algumas contas mais novas recusam o pacote legado de quatro campos.
				// Repetimos uma única vez exatamente como o cliente oficial atual:
				// cabeçalho 756 e dois campos (e-mail e senha).
				if err := sendEncrypted(conn, cryptoState, tryLoginCurrent,
					mustString(credentials.Email), mustString(credentials.Password)); err != nil {
					return fmt.Errorf("ERRO_LOGIN: não foi possível tentar o fluxo atual de autenticação (%w)", err)
				}
				currentLoginTried = true
				fmt.Println("MARCO_OK: NO_LOGIN_PERMISSION no fluxo legado; TRY_LOGIN atual enviado com dois campos")
				continue
			}
			return fmt.Errorf("ERRO_LOGIN: servidor exige um código de uso único enviado por e-mail; conclua esse acesso no cliente oficial e tente novamente no Headless")
		case loginIncorrect:
			return fmt.Errorf("ERRO_LOGIN: login ou senha incorretos")
		case totpRequired:
			return fmt.Errorf("ERRO_LOGIN: a conta exige verificação de dois fatores por e-mail")
		case userBanned:
			return fmt.Errorf("ERRO_LOGIN: acesso recusado pelo servidor para esta conta")
		default:
			// Alguns erros de autenticação do Origins chegam imediatamente antes
			// do encerramento do socket. Registrar apenas a resposta do servidor
			// (nunca as credenciais enviadas) permite transformar o EOF genérico em
			// uma mensagem útil no dashboard.
			log.Printf("TRACE_LOGIN_REPLY header=%d payload=%x", header, packet[2:])
		}
	}
}

func loginPayload(credentials loginConfig) [][]byte {
	// Formato comprovado pelo executável que autenticou a Nubank em 23/08/2026:
	// login, senha, código 2FA (vazio quando ausente) e identificador legado vazio.
	return [][]byte{
		mustString(credentials.Email),
		mustString(credentials.Password),
		mustString(credentials.TOTP),
		mustString(""),
	}
}

type encryptedPacketReader struct {
	conn      net.Conn
	encrypted *origins.EncryptedServerBuffer
	packets   *origins.PlainServerBuffer
	chunk     []byte
}

func newEncryptedPacketReader(conn net.Conn, cryptoState *origins.Crypto) *encryptedPacketReader {
	return &encryptedPacketReader{
		conn: conn, encrypted: origins.NewEncryptedServerBuffer(cryptoState),
		packets: &origins.PlainServerBuffer{}, chunk: make([]byte, 8192),
	}
}

func (r *encryptedPacketReader) Next() ([]byte, error) {
	for {
		if packet, ok := r.packets.Next(); ok {
			return packet, nil
		}
		for {
			plain, ok, err := r.encrypted.Next()
			if err != nil {
				return nil, err
			}
			if !ok {
				break
			}
			r.packets.Push(plain)
		}
		if packet, ok := r.packets.Next(); ok {
			return packet, nil
		}
		n, err := r.conn.Read(r.chunk)
		if err != nil {
			return nil, err
		}
		r.encrypted.Push(r.chunk[:n])
	}
}

func mustString(value string) []byte {
	data, err := origins.OutgoingString(value)
	if err != nil {
		panic(err)
	}
	return data
}

func replyPong(conn net.Conn, cryptoState *origins.Crypto, _ []byte) error {
	// O protocolo Origins usa PONG sem conteúdo. Dados extras aqui fazem o
	// servidor encerrar a sessão no próximo heartbeat, derrubando a conta no
	// meio da entrada do quarto ou de qualquer automação.
	return sendEncrypted(conn, cryptoState, pong)
}

func sendEncrypted(conn net.Conn, cryptoState *origins.Crypto, header int, payload ...[]byte) error {
	packet, err := origins.Packet(header, payload...)
	if err != nil {
		return err
	}
	frame, err := cryptoState.EncryptClient(packet)
	if err != nil {
		return err
	}
	_, err = conn.Write(frame)
	log.Printf("enviado criptografado header=%d bytes=%d", header, len(packet)-2)
	return err
}

func awaitSessionParameters(reader *encryptedPacketReader) error {
	for {
		packet, err := reader.Next()
		if err != nil {
			return err
		}
		header, err := origins.Header(packet)
		if err != nil {
			return err
		}
		log.Printf("recebido criptografado header=%d bytes=%d", header, len(packet)-2)
		if header == sessionParams {
			return nil
		}
	}
}

func sendPlain(conn net.Conn, header int, payload ...[]byte) error {
	packet, err := origins.Packet(header, payload...)
	if err != nil {
		return err
	}
	frame, err := origins.ClientFrame(packet)
	if err != nil {
		return err
	}
	_, err = conn.Write(frame)
	log.Printf("enviado header=%d bytes=%d", header, len(packet)-2)
	return err
}

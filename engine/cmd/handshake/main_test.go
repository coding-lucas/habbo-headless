package main

import (
	"bytes"
	"encoding/base64"
	mathrand "math/rand"
	"os"
	"strings"
	"testing"
	"time"

	"habbo-headless/engine/internal/origins"
)

func TestParseFlatResultsOrdenaPorOcupacao(t *testing.T) {
	payload := []byte("42\tSala menor\tDonoA\topen\t0\t7\t25\t\tDescrição A\r99\tSala cheia\tDonoB\topen\t0\t21\t25\t\tDescrição B\r\x02")
	rooms, err := parseFlatResults(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(rooms) != 2 || rooms[0].ID != 99 || rooms[0].Users != 21 || rooms[1].ID != 42 {
		t.Fatalf("catálogo inesperado: %+v", rooms)
	}
}

func TestParseRecommendedRoomListOrdenaEMapeiaAcesso(t *testing.T) {
	incomingString := func(value string) []byte { return append([]byte(value), 2) }
	payload := append([]byte{}, origins.EncodeVL64(2)...)
	payload = append(payload, origins.EncodeVL64(42)...)
	payload = append(payload, incomingString("Sala menor")...)
	payload = append(payload, incomingString("DonoA")...)
	payload = append(payload, incomingString("open")...)
	payload = append(payload, origins.EncodeVL64(7)...)
	payload = append(payload, origins.EncodeVL64(25)...)
	payload = append(payload, incomingString("Descrição A")...)
	payload = append(payload, origins.EncodeVL64(99)...)
	payload = append(payload, incomingString("Sala cheia")...)
	payload = append(payload, incomingString("DonoB")...)
	payload = append(payload, incomingString("password")...)
	payload = append(payload, origins.EncodeVL64(21)...)
	payload = append(payload, origins.EncodeVL64(25)...)
	payload = append(payload, incomingString("Descrição B")...)

	rooms, err := parseRecommendedRoomList(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(rooms) != 2 || rooms[0].ID != 99 || rooms[0].Users != 21 || rooms[1].ID != 42 {
		t.Fatalf("catálogo inesperado: %+v", rooms)
	}
	if rooms[0].Access != "password" || rooms[1].Access != "open" {
		t.Fatalf("acessos inesperados: %+v", rooms)
	}
}

func TestRandomPartyTileEvitaPortaEBloqueios(t *testing.T) {
	rng := mathrand.New(mathrand.NewSource(1))
	heightmap := []string{"xxxxx", "x000x", "x000x", "xxxxx"}
	x, y, ok := randomPartyTile(rng, heightmap, 1, 1, 2, 1)
	if !ok {
		t.Fatal("nenhum destino encontrado")
	}
	if heightmap[y][x] == 'x' || (x == 2 && y == 1) || (x == 1 && y == 1) {
		t.Fatalf("destino inválido: %d,%d", x, y)
	}
}

func TestEstadoPescaDoAlvoAtual(t *testing.T) {
	t.Parallel()

	estado, ok := estadoPesca("mv 25,21,0.0/fsh 22,18,0,1/", 22, 18)
	if !ok || estado != "1" {
		t.Fatalf("estadoPesca() = %q, %v; esperado 1, true", estado, ok)
	}
}

func TestParseFishingStatsLeOsSeteCamposDoHotel(t *testing.T) {
	values := []int{42, 100, 91500, 84512, 93900, 3210, 18}
	payload := make([]byte, 0)
	for _, value := range values {
		payload = append(payload, origins.EncodeVL64(value)...)
	}
	got, err := parseFishingStats(payload)
	if err != nil {
		t.Fatal(err)
	}
	if got.CurrentLevel != 42 || got.MaximumLevel != 100 || got.TotalXP != 91500 ||
		got.XPForCurrentLevel != 84512 || got.XPForNextLevel != 93900 ||
		got.FishesCaught != 3210 || got.GoldenFishesCaught != 18 {
		t.Fatalf("estatísticas incorretas: %+v", got)
	}
}

func TestPayloadDeLoginMantemCompatibilidadeEFluxoAtual(t *testing.T) {
	credentials := loginConfig{Email: "conta@exemplo.com", Password: "senha", TOTP: "123456"}
	legacy := loginPayload(credentials)
	if len(legacy) != 4 {
		t.Fatalf("payload legado possui %d campos; esperado 4", len(legacy))
	}
	if tryLoginCurrent != 756 || noLoginPermission != 20 {
		t.Fatalf("cabeçalhos de fallback divergentes: login=%d permissão=%d", tryLoginCurrent, noLoginPermission)
	}
}

func TestEstadoPescaIgnoraOutroAlvo(t *testing.T) {
	t.Parallel()

	if estado, ok := estadoPesca("/fsh 22,18,0,1/", 21, 16); ok {
		t.Fatalf("estadoPesca() aceitou outro alvo: %q", estado)
	}
}

func TestFallbackDeUsuarioNaoSobrescreveContaJaIdentificada(t *testing.T) {
	t.Parallel()
	entities := []roomUser{{index: 12, name: "OutroJogador", kind: 1}}
	if shouldUseSingleUserFallback("", 8, entities) {
		t.Fatal("fallback tentou substituir o índice da conta já identificada")
	}
}

func TestFallbackDeUsuarioSoValeAntesDaIdentificacao(t *testing.T) {
	t.Parallel()
	entities := []roomUser{{index: 8, name: "Conta", kind: 1}}
	if !shouldUseSingleUserFallback("", -1, entities) {
		t.Fatal("fallback recusou o único avatar antes da identificação")
	}
}

func TestFallbackDeUsuarioNaoRodaQuandoNomeOficialExiste(t *testing.T) {
	t.Parallel()
	entities := []roomUser{{index: 12, name: "OutroJogador", kind: 1}}
	if shouldUseSingleUserFallback("ContaOficial", -1, entities) {
		t.Fatal("fallback tentou substituir a conta cujo nome oficial já era conhecido")
	}
}

func TestAlcanceHeadlessUsaFaixaSeguraDoServidor(t *testing.T) {
	t.Parallel()
	if alcancePesca != 2.5 {
		t.Fatalf("alcancePesca = %.1f; esperado 2.5", alcancePesca)
	}
}

func TestPuxadaMantemCadenciaRapidaDoV72(t *testing.T) {
	t.Parallel()
	if maximoReforcosPuxada != 1 || intervaloReforcoPuxada != 1200*time.Millisecond {
		t.Fatalf("cadência divergente das capturas reais: intervalo=%v envios=%d", intervaloReforcoPuxada, maximoReforcosPuxada)
	}
	if timeoutResultadoPuxada != 12*time.Second {
		t.Fatalf("watchdog pós-puxada diverge das capturas reais: %s", timeoutResultadoPuxada)
	}
}

func TestWatchdogsNaoCancelamAntesDaMordida(t *testing.T) {
	if timeoutAguardandoMordida < 20*time.Second {
		t.Fatalf("timeout de mordida curto demais: %v", timeoutAguardandoMordida)
	}
	if timeoutResultadoPuxada < 10*time.Second {
		t.Fatalf("resultado da puxada precisa comportar a animação completa: %v", timeoutResultadoPuxada)
	}
	if timeoutPescaSemResposta >= timeoutResultadoPuxada {
		t.Fatalf("lançamento sem confirmação deve recuperar primeiro: %v >= %v", timeoutPescaSemResposta, timeoutResultadoPuxada)
	}
}

func TestTimeoutDaPescaSegueAFaseConfirmada(t *testing.T) {
	t.Parallel()
	if got := fishingTimeoutForPhase(fishingPhaseCastSent); got != timeoutPescaSemResposta {
		t.Fatalf("lançamento sem confirmação usa %s; esperado %s", got, timeoutPescaSemResposta)
	}
	if got := fishingTimeoutForPhase(fishingPhaseWaitingBite); got != timeoutAguardandoMordida {
		t.Fatalf("mordida usa %s; esperado %s", got, timeoutAguardandoMordida)
	}
	if got := fishingTimeoutForPhase(fishingPhasePullSent); got != timeoutResultadoPuxada {
		t.Fatalf("resultado usa %s; esperado %s", got, timeoutResultadoPuxada)
	}
	if cooldown := fishingTimeoutCooldown(fishingPhasePullSent); cooldown <= cooldownPeixeSemResposta {
		t.Fatalf("resultado perdido deveria deixar uma quarentena maior: %s", cooldown)
	}
}

func TestMaquinaDeEstadosMantemMesmoPeixeEntreMordidas(t *testing.T) {
	if !devePuxar("0", "1", false) {
		t.Fatal("transição 0→1 deveria puxar")
	}
	if devePuxar("1", "0", true) {
		t.Fatal("retorno 1→0 não deve enviar outra puxada")
	}
	if devePuxar("0", "1", true) {
		t.Fatal("uma quarta puxada não pode ser enviada antes do resultado")
	}
	if !rodadaDePescaReiniciada("1", "0", true) {
		t.Fatal("retorno 1→0 deve preservar a janela de reforços no mesmo peixe")
	}
	if rodadaDePescaReiniciada("0", "1", false) {
		t.Fatal("mordida não pode ser confundida com nova rodada")
	}
}

func TestMensagemDeCapturaEncerraCicloSemEsperarEndFishing(t *testing.T) {
	t.Parallel()
	message := "Você pegou um Crappie! (+47 EXP)"
	if !strings.Contains(strings.ToUpper(message), "EXP") {
		t.Fatal("mensagem de captura deixou de ser reconhecida")
	}
}

func TestLancamentoInicialDuplicaPacoteNoHeadless(t *testing.T) {
	fonte, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	bloco := string(fonte)
	inicio := strings.Index(bloco, "if podeLancar(")
	fim := strings.Index(bloco[inicio:], "if alvo != 0 && lancamento.IsZero()")
	if inicio < 0 || fim < 0 {
		t.Fatal("não foi possível localizar o bloco de lançamento")
	}
	bloco = bloco[inicio : inicio+fim]
	if got := strings.Count(bloco, "sendEncrypted(conn, cryptoState, startFishing"); got != 2 {
		t.Fatalf("lançamento inicial envia %d pacotes; esperado exatamente 2", got)
	}
}

func TestLancaAoEntrarNoAlcanceMesmoComStatusDeMovimento(t *testing.T) {
	t.Parallel()
	if !podeLancar(10, time.Time{}, true, true, 19, 15, 21, 16) {
		t.Fatal("podeLancar() ficou esperando um STATUS de parada já dentro do alcance")
	}
}

func TestLancaParadoDentroDoAlcanceSeguro(t *testing.T) {
	t.Parallel()
	if !podeLancar(10, time.Time{}, true, false, 19, 15, 21, 16) {
		t.Fatal("podeLancar() recusou avatar parado dentro do alcance")
	}
}

func TestDestinoDePescaReplicaAproximacaoDoV72(t *testing.T) {
	t.Parallel()
	x, y := legacyFishingDestination(15, 22, 43, 4)
	if x != 41 || y != 6 {
		t.Fatalf("legacyFishingDestination() = %d,%d; esperado 41,6", x, y)
	}
}

func TestDestinoDePescaMantemEixoJaProximo(t *testing.T) {
	t.Parallel()
	x, y := legacyFishingDestination(20, 14, 22, 26)
	if x != 22 || y != 24 {
		t.Fatalf("legacyFishingDestination() = %d,%d; esperado 22,24", x, y)
	}
}

func TestDestinoHeadlessEscolhePisoAlcancavel(t *testing.T) {
	t.Parallel()
	heightmap := []string{
		"xxxxxxxxxx",
		"x00000000x",
		"x00000000x",
		"x00000000x",
		"xxxxxxxxxx",
	}
	x, y, caminho, ok := fishingDestination(heightmap, map[[2]int]time.Time{}, 1, 1, 8, 2)
	if !ok || caminho <= 0 || heightmap[y][x] == 'x' || distancia(x, y, 8, 2) > alcancePesca {
		t.Fatalf("destino inválido: %d,%d caminho=%d ok=%v", x, y, caminho, ok)
	}
}

func TestDestinoHeadlessDesviaDePisoBloqueadoSemTrocarPeixe(t *testing.T) {
	t.Parallel()
	heightmap := []string{
		"xxxxxxxxxx",
		"x00000000x",
		"x00000000x",
		"x00000000x",
		"xxxxxxxxxx",
	}
	blocked := map[[2]int]time.Time{}
	x1, y1, _, ok := fishingDestination(heightmap, blocked, 1, 1, 8, 2)
	if !ok {
		t.Fatal("primeiro destino não encontrado")
	}
	blocked[[2]int{x1, y1}] = time.Now().Add(time.Minute)
	x2, y2, _, ok := fishingDestination(heightmap, blocked, 1, 1, 8, 2)
	if !ok || (x1 == x2 && y1 == y2) {
		t.Fatalf("destino alternativo inválido: primeiro=%d,%d segundo=%d,%d ok=%v", x1, y1, x2, y2, ok)
	}
}

func TestLoginHabboUsaFormatoValidadoDeQuatroCampos(t *testing.T) {
	t.Parallel()
	payload := loginPayload(loginConfig{Email: "conta@exemplo.com", Password: "segredo", TOTP: "123456"})
	if len(payload) != 4 {
		t.Fatalf("loginPayload() gerou %d campos; esperado 4", len(payload))
	}
	if !bytes.Equal(payload[0], mustString("conta@exemplo.com")) ||
		!bytes.Equal(payload[1], mustString("segredo")) ||
		!bytes.Equal(payload[2], mustString("123456")) ||
		!bytes.Equal(payload[3], mustString("")) {
		t.Fatalf("loginPayload() gerou campos inesperados")
	}
}

func TestLoginSteamUsaDoisCamposOficiais(t *testing.T) {
	t.Parallel()
	steamID := "76561190000000000"
	ticket := "0123456789abcdef"
	expectedID, _ := origins.OutgoingString(steamID)
	expectedTicket, _ := origins.OutgoingString(ticket)
	if !bytes.Equal(mustString(steamID), expectedID) {
		t.Fatal("Steam ID não foi codificado como string do protocolo")
	}
	if !bytes.Equal(mustString(ticket), expectedTicket) {
		t.Fatal("ticket Steam não foi codificado como string do protocolo")
	}
}

func TestBloqueioDeRotaExpira(t *testing.T) {
	t.Parallel()
	tile := [2]int{2, 3}
	blocked := map[[2]int]time.Time{tile: time.Now().Add(-time.Millisecond)}
	if tileTemporariamenteBloqueado(blocked, tile) {
		t.Fatal("piso continuou bloqueado após o vencimento")
	}
	if _, exists := blocked[tile]; exists {
		t.Fatal("bloqueio vencido não foi removido")
	}
}

func TestFallbackDiretoEncontraPisoNoAlcance(t *testing.T) {
	t.Parallel()
	heightmap := []string{
		"xxxxxxxxxx",
		"x00000000x",
		"x00000000x",
		"x00000000x",
		"xxxxxxxxxx",
	}
	x, y, ok := directFishingTile(heightmap, 1, 1, 8, 2)
	if !ok {
		t.Fatal("fallback não encontrou piso pescável")
	}
	if distancia(x, y, 8, 2) > alcancePesca {
		t.Fatalf("piso %d,%d ficou fora do alcance", x, y)
	}
}

func TestPeixeComMenorCaminhoVenceMesmoSeVisualmenteMaisDistante(t *testing.T) {
	t.Parallel()
	if !betterFishCandidate(7, 2, 10, 3, 8, 20) {
		t.Fatal("peixe com caminho menor não recebeu prioridade")
	}
}

func TestPeixeComCaminhoMaiorNaoSubstituiRotaCurta(t *testing.T) {
	t.Parallel()
	if betterFishCandidate(3, 8, 20, 7, 2, 10) {
		t.Fatal("peixe com caminho maior venceu apenas por estar visualmente perto")
	}
}

func TestPeixeSemRespostaFicaForaDaProximaEscolha(t *testing.T) {
	t.Parallel()
	agora := time.Now()
	areas := map[int]fishingArea{
		10: {id: 10, x: 4, y: 4},
		11: {id: 11, x: 5, y: 4},
	}
	blocked := map[int]time.Time{10: agora.Add(cooldownPeixeSemResposta)}
	available := availableFishingAreas(areas, blocked, agora)
	if _, exists := available[10]; exists {
		t.Fatal("peixe sem resposta continuou elegível durante a quarentena")
	}
	if _, exists := available[11]; !exists {
		t.Fatal("peixe alternativo foi removido indevidamente")
	}
	available = availableFishingAreas(areas, blocked, agora.Add(cooldownPeixeSemResposta+time.Millisecond))
	if _, exists := available[10]; !exists {
		t.Fatal("peixe não voltou a ficar elegível após a quarentena")
	}
}

func TestNaoDesviaEnquantoAvatarAindaEstaCaminhando(t *testing.T) {
	t.Parallel()
	if shouldRetryFishingMovement(true, tentativasAntesDesvio+2) {
		t.Fatal("rota foi marcada para desvio enquanto o avatar ainda caminhava")
	}
	if shouldRetryFishingMovement(false, tentativasAntesDesvio-1) {
		t.Fatal("rota foi desviada antes de confirmar parada")
	}
	if !shouldRetryFishingMovement(false, tentativasAntesDesvio) {
		t.Fatal("rota parada não foi revisada após o limite")
	}
}

func TestRecuperaSalaSomenteAposResyncsVaziosComMapaPronto(t *testing.T) {
	t.Parallel()
	if shouldRecoverFishingRoom(false, []string{"000"}, maxResyncsSemPeixe) {
		t.Fatal("recuperação acionou antes de a posição estar disponível")
	}
	if shouldRecoverFishingRoom(true, nil, maxResyncsSemPeixe) {
		t.Fatal("recuperação acionou antes de o mapa estar disponível")
	}
	if shouldRecoverFishingRoom(true, []string{"000"}, maxResyncsSemPeixe-1) {
		t.Fatal("recuperação acionou antes do limite de ressincronizações")
	}
	if !shouldRecoverFishingRoom(true, []string{"000"}, maxResyncsSemPeixe) {
		t.Fatal("sessão sem objetos não entrou em recuperação após o limite")
	}
}

func TestAlvoMaisProximoTambemPrecisaTerRotaPescavel(t *testing.T) {
	t.Parallel()
	heightmap := []string{
		"xxxxxxxxxxxx",
		"x0000xxxx00x",
		"x0000xxxx00x",
		"x0000xxxx00x",
		"xxxxxxxxxxxx",
	}
	areas := map[int]fishingArea{
		10: {id: 10, x: 7, y: 2}, // fisicamente perto, mas isolado pela parede.
		20: {id: 20, x: 3, y: 2},
	}
	target, ok := closestReachableFishingTarget(heightmap, map[[2]int]time.Time{}, 1, 2, areas)
	if !ok || target.id != 20 {
		t.Fatalf("alvo acessível = %d (ok=%v); esperado 20", target.id, ok)
	}
}

func TestRemocaoVisualNaoLiberaPescaEmAndamento(t *testing.T) {
	t.Parallel()
	if shouldReleaseRemovedTarget(time.Now()) {
		t.Fatal("remoção visual liberou movimento antes do término da pesca")
	}
	if !shouldReleaseRemovedTarget(time.Time{}) {
		t.Fatal("alvo removido antes do lançamento deveria ser liberado")
	}
}

func TestPerfilRetornaNicknameReal(t *testing.T) {
	t.Parallel()
	payload := append(origins.EncodeVL64(123), append([]byte("NomeReal"), 2)...)
	name, err := parseOwnProfileName(payload)
	if err != nil || name != "NomeReal" {
		t.Fatalf("parseOwnProfileName() = %q, %v; esperado NomeReal", name, err)
	}
}

func TestNavegadorLocalizaOsTresDestinosDePesca(t *testing.T) {
	t.Parallel()
	payload := []byte{}
	appendInt := func(value int) { payload = append(payload, origins.EncodeVL64(value)...) }
	appendString := func(value string) {
		payload = append(payload, []byte(value)...)
		payload = append(payload, 2)
	}
	appendCategory := func(id, parent int, name string) {
		appendInt(id)
		appendInt(0)
		appendString(name)
		appendInt(0)
		appendInt(500)
		appendInt(parent)
	}
	appendRoom := func(id, parent, port, door int, name string) {
		appendInt(id)
		appendInt(1)
		appendString(name)
		appendInt(0)
		appendInt(100)
		appendInt(parent)
		appendString("unidade")
		appendInt(port)
		appendInt(door)
		appendString("hh_room")
		appendInt(0)
		appendInt(1)
	}

	appendInt(1) // máscara
	appendCategory(3, 0, "Espaços Públicos")
	appendCategory(18, 3, "Fora do Hotel")
	appendRoom(40, 18, 1, 1, "Parque Infobus")
	appendRoom(41, 18, 1, 2, "Jardim Flutuante")
	appendRoom(42, 18, 1, 3, "Snouthill Pier")

	nodes, err := parseNavigatorNodes(payload)
	if err != nil {
		t.Fatal(err)
	}
	for destination, config := range fishingRooms {
		found := false
		for _, node := range nodes {
			if node.Room != nil && matchesFishingRoom(node.Room.Name, config) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("destino %s não foi localizado", destination)
		}
	}
}

func TestJardimFlutuanteAceitaNomeAlternativoPortHana(t *testing.T) {
	t.Parallel()
	if !matchesFishingRoom("Port Hana", fishingRooms["jardim-flutuante"]) {
		t.Fatal("nome internacional atual do quarto intermediário não foi reconhecido")
	}
}

func TestNomeDeQuartoIgnoraAcentosEMaiusculas(t *testing.T) {
	t.Parallel()
	if !matchesFishingRoom("JARDÍN FLOTANTE", fishingRooms["jardim-flutuante"]) {
		t.Fatal("variante localizada do Jardim Flutuante não foi reconhecida")
	}
}

func TestDecodificaMensagemGlobalComAcentos(t *testing.T) {
	command := globalChatCommandPrefix + base64.RawStdEncoding.EncodeToString([]byte("Olá, pessoal! 🎣"))
	message, handled, err := decodeGlobalChatCommand(command)
	if err != nil || !handled || message != "Olá, pessoal! 🎣" {
		t.Fatalf("decodificação inesperada: texto=%q handled=%v err=%v", message, handled, err)
	}
}

func TestMensagemGlobalMultilinhaEhRecusada(t *testing.T) {
	command := globalChatCommandPrefix + base64.RawStdEncoding.EncodeToString([]byte("uma\noutra"))
	if _, handled, err := decodeGlobalChatCommand(command); !handled || err == nil {
		t.Fatal("mensagem multilinha foi aceita")
	}
}

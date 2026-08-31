# Habbo Headless

Painel local para controlar sessões e a automação de pesca do Habbo Hotel Origins sem renderizar o cliente gráfico.

> Projeto privado e independente, sem vínculo oficial com a Sulake ou com o Habbo. Use apenas em contas e ambientes sob sua responsabilidade e respeite os termos do serviço.

## Arquitetura validada

- O dashboard roda em `http://localhost:3000`.
- O motor Go roda em `http://127.0.0.1:8787` e mantém uma sessão de protocolo independente por conta.
- Cada sessão inicia automaticamente uma instância invisível do Codex G-Earth como proxy transparente.
- A pesca é executada pelo motor Go headless; a extensão gráfica antiga não é iniciada.
- O relatório em `http://127.0.0.1:8788` acompanha capturas, XP, espécies, média e estado por sessão.
- Ao desconectar uma conta, handshake e Codex G-Earth daquela sessão são encerrados juntos.

## Login

### Steam

Fluxo comprovado em produção local:

`HELLO → criptografia → SESSION_PARAMETERS → STEAM_OPENID_LOGIN → autorização oficial → RIGHTS → LOGIN_OK`

A autorização abre a página oficial da Steam no navegador. Nenhum Habbo Launcher ou cliente gráfico é iniciado, e a URL sensível não é gravada nem retornada na listagem da API.

### Conta Habbo

O motor envia `TRY_LOGIN` com login, senha, código 2FA opcional e o quarto campo legado. Um login só é salvo depois de o servidor retornar `LOGIN_OK`. A senha persistida é protegida pelo DPAPI do usuário atual do Windows.

## Pesca

Ao iniciar, o painel permite escolher Infobus, Jardim Flutuante ou Snouthill Pier para uma, várias ou todas as contas. O motor percorre o navegador público, reconhece nomes localizados e entra no destino selecionado antes de sincronizar usuário e áreas de pesca.

Em seguida, escolhe o peixe com a menor rota realmente caminhável até um piso de pesca; a distância visual só desempata rotas equivalentes. Esse alvo fica travado durante toda a aproximação: o motor mantém a mesma coordenada de destino, lança assim que entra no alcance e não escolhe outro peixe até a captura, `END_FISHING`, desaparecimento anterior ao lançamento ou watchdog real.

Quando várias contas pescam na mesma instância, o motor reserva o alvo por uma janela curta antes de lançar a vara. Assim duas contas do mesmo painel não insistem no mesmo peixe. Se o navegador público expuser mais de uma instância compatível, as contas também são distribuídas primeiro entre salas diferentes e depois pela menor ocupação.

As atualizações de posição recebidas quadrado a quadrado são animações do servidor. Se for necessário reforçar uma caminhada, o motor reenvia a mesma coordenada final; ele não fragmenta a rota nem alterna entre peixes durante o percurso.

Os pacotes `ACTIVEOBJECT_ADD` e `ACTIVEOBJECT_REMOVE` são a fonte autoritativa dos peixes. Uma sincronização vazia não apaga os peixes já monitorados, evitando que o próprio resync faça o bot abandonar um alvo válido.

Na sessão sem cliente gráfico, o lançamento inicial usa dois `STARTFISHING`. Quando ocorre a mordida (`0 → 1`), a puxada usa um envio imediato comprovado em captura real, sem repetir pacotes durante a animação. O alvo continua travado durante toda a resolução; o watchdog é de 4,2 s para lançamento sem resposta, 25 s aguardando mordida e 12 s aguardando o resultado da puxada. Os relatórios distinguem essas três fases para que um timeout não pareça apenas baixa produtividade.

## Iniciar e parar

O painel permite selecionar uma, várias ou todas as contas elegíveis. A parada só é exibida como concluída depois de cada processo responder `FISHING_STOPPED`; falha ou confirmação ausente aparece como erro, sem encerrar silenciosamente o modal. A sessão permanece conectada após parar e pode ser iniciada novamente sem refazer o login.

## Desenvolvimento

```powershell
npm run build
npm run start
```

```powershell
cd engine
go test ./...
go build -o engine.exe .
go build -o ..\bin\habbo-handshake.exe .\cmd\handshake
```

O inicializador do Desktop é compilado a partir de `launcher` e inicia motor, relatórios e dashboard sem abrir terminais duplicados.

O núcleo integrado deriva do G-Earth e mantém a licença em `runtime/codex-gearth/LICENSE-G-EARTH.txt`.

## Segurança e backup

Este repositório guarda somente o código-fonte. Credenciais, logins salvos, relatórios de pesca, registros locais, executáveis compilados, runtimes e certificados não são versionados.

Antes de executar em outra máquina:

1. instale Node.js 22.13 ou superior e Go;
2. restaure ou obtenha separadamente o runtime autorizado do G-Earth;
3. compile o painel, motor, handshake, relatórios e inicializador;
4. configure as contas novamente no ambiente local.

Nunca envie `data/saved-logins.json`, arquivos de ambiente, logs ou pacotes portáteis ao GitHub.

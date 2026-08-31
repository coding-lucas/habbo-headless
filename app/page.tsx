"use client";
/* eslint-disable @next/next/no-img-element -- As prévias dos presets são imagens locais controladas pelo próprio painel. */
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import {
  BarChart3,
  Bot,
  LayoutDashboard,
  LogIn,
  MapPinned,
  Palette,
  Radio,
  Shapes,
  UsersRound,
  Zap,
} from "lucide-react";
type Session = {
  id: string;
  profileId?: string;
  nickname: string;
  status: string;
  loginMode: string;
  automation?: string;
  automationId?: "pesca" | "festa" | "formacao";
  automationTarget?: string;
  automationStartedAt?: string;
  automationSeconds: number;
  automationStarts: number;
  fishingDestination?: FishingDestination;
  fishingRoom?: string;
  lastEvent?: string;
  disconnectedReason?: string;
  authorizationReady?: boolean;
};
type Profile = { id: string; nickname: string; mode: string; email: string };
type BulkLoginState = {
  running: boolean;
  paused: boolean;
  total: number;
  completed: number;
  connected: number;
  failed: number;
  current: string;
};
type Bot = { id: string; name: string; description: string; mode: string };
type GEarthStatus = {
  running: boolean;
  ready: boolean;
  connected: boolean;
  instances?: number;
  state: string;
};
type Capture = {
  at: string;
  accountId: string;
  account: string;
  sessionId: string;
  fish: string;
  xp: number;
};
type AccountReport = {
  accountId: string;
  sessionId: string;
  account: string;
  loginMode: string;
  captures: number;
  xp: number;
  casts: number;
  timeouts: number;
  pulls: number;
  routesBlocked: number;
  targetContentions: number;
  noAckTimeouts: number;
  biteTimeouts: number;
  resultTimeouts: number;
  fishSlipped: number;
  automationSeconds: number;
  sessions: number;
  fish: Record<string, number>;
  lastCaptureAt?: string;
  currentStatus: string;
  currentlyFishing: boolean;
  fishingLevel?: number;
  fishingLevelMin?: number;
  fishingLevelMax?: number;
  fishingLevelSource?: string;
  fishingTotalXp?: number;
  fishingXpForCurrentLevel?: number;
  fishingXpForNextLevel?: number;
  fishingFishesCaught?: number;
  fishingGoldenFishesCaught?: number;
  fishPerMinute: number;
  xpPerHour: number;
  successRate: number;
};
type FishingReport = {
  startedAt: string;
  updatedAt: string;
  totals: AccountReport;
  accounts: AccountReport[];
  captures: Capture[];
};
type AppearancePreset = {
  id: string;
  name: string;
  description: string;
  figure: string;
  preview: string;
};
type AppearanceState = {
  selectedId: string;
  presets: AppearancePreset[];
};
type Room = {
  id: number;
  name: string;
  owner: string;
  access: string;
  users: number;
  capacity: number;
  description: string;
  port?: number;
  door?: number;
};
type RoomCatalog = {
  rooms: Room[];
  ownerQuery?: string;
  updatedAt?: string;
  refreshing: boolean;
};
type View = "overview" | "accounts" | "automations" | "rooms" | "formations" | "appearance" | "reports";
type FishingDestination = "infobus" | "jardim-flutuante" | "snouthill-pier";
type FishingMode = "manual" | "por-nivel";
type FormationShape = "coracao" | "estrela" | "coroa" | "peixe" | "trofeu" | "smile" | "help";
const fishingDestinations: {
  id: FishingDestination;
  name: string;
  level: string;
  description: string;
}[] = [
  {
    id: "infobus",
    name: "Infobus",
    level: "Níveis 1–29",
    description: "Área inicial de pesca.",
  },
  {
    id: "jardim-flutuante",
    name: "Jardim Flutuante",
    level: "Níveis 30–69",
    description: "Faixa intermediária de pesca.",
  },
  {
    id: "snouthill-pier",
    name: "Snouthill Pier",
    level: "Nível 70+",
    description: "Área avançada de pesca.",
  },
];
const fishingDestinationLabel = (destination?: string) =>
  fishingDestinations.find((room) => room.id === destination)?.name || "Destino não informado";
const formationShapes: { id: FormationShape; name: string; preview: string; description: string }[] = [
  { id: "coracao", name: "Coração grande", preview: "♥", description: "Formação romântica para recepção e palco." },
  { id: "estrela", name: "Estrela", preview: "★", description: "Destaque de abertura ou premiação." },
  { id: "coroa", name: "Coroa", preview: "♛", description: "Pódio, realeza e vencedores." },
  { id: "peixe", name: "Peixe", preview: "🐟", description: "Perfeita para eventos de pesca." },
  { id: "trofeu", name: "Troféu", preview: "🏆", description: "Celebração de meta ou resultado." },
  { id: "smile", name: "Smile", preview: "☺", description: "Carinha sorrindo para boas-vindas." },
  { id: "help", name: "HELP", preview: "HELP", description: "Palavra em fonte pixelada." },
];
const fishingLevelLabel = (account: AccountReport) => {
  if (account.fishingLevel) return `Nível ${account.fishingLevel}`;
  if (account.fishingLevelMin) return `Nível ${account.fishingLevelMin}+`;
  if (account.fishingLevelMax) return `Nível 1–${account.fishingLevelMax}`;
  return "Aguardando leitura";
};
const api = "http://127.0.0.1:8787",
  reportApi = "http://127.0.0.1:8788",
  connected = [
    "conectada",
    "no-infobus",
    "visível-no-infobus",
    "pescando-no-infobus",
    "no-quarto-pesca",
    "aguardando-peixe",
    "indo-pescar",
    "vara-lançada",
    "no-quarto-festa",
    "festejando",
    "passeando",
    "dançando",
    "no-quarto-formacao",
    "formando-fila",
    "formando-figura",
    "organizando-formacao",
    "saindo-do-quarto",
  ];
const profileName = (profile: Profile) =>
  !profile.nickname || profile.nickname.toLowerCase().startsWith("identificando")
    ? profile.email
    : profile.nickname;
const accountKey = (value?: string) => (value || "").trim().toLocaleLowerCase("pt-BR");
const reportAccountKey = (account: Pick<AccountReport, "accountId" | "sessionId">) =>
  account.accountId || `session:${account.sessionId}`;
const captureAccountKey = (capture: Pick<Capture, "accountId" | "sessionId">) =>
  capture.accountId || `session:${capture.sessionId}`;
const profileHasSession = (profile: Profile, sessions: Session[]) => {
  const expectedNames = new Set([accountKey(profile.nickname), accountKey(profile.email)].filter(Boolean));
  return sessions.some(
    (session) =>
      session.status !== "desconectada" &&
      session.status !== "encerrada" &&
			session.status !== "aguardando-verificacao" &&
      (session.profileId === profile.id || expectedNames.has(accountKey(session.nickname))),
  );
};
const duration = (n: number) =>
  `${String(Math.floor(n / 3600)).padStart(2, "0")}:${String(Math.floor((n % 3600) / 60)).padStart(2, "0")}:${String(n % 60).padStart(2, "0")}`;
const number = (n: number, digits = 0) =>
  new Intl.NumberFormat("pt-BR", { maximumFractionDigits: digits }).format(n);
const when = (value?: string) =>
  value
    ? new Intl.DateTimeFormat("pt-BR", {
        day: "2-digit",
        month: "2-digit",
        hour: "2-digit",
        minute: "2-digit",
        second: "2-digit",
      }).format(new Date(value))
    : "—";
const statusLabel = (status: string) =>
  ({
    conectada: "Conectada",
    "no-infobus": "No Infobus",
    "no-quarto-pesca": "No quarto de pesca",
    "aguardando-peixe": "Procurando peixe",
    "indo-pescar": "Indo até o peixe",
    "vara-lançada": "Vara lançada",
    pescando: "Pescando",
    "iniciando-pesca": "Iniciando pesca",
    parando: "Parando pesca…",
    "no-quarto-festa": "No quarto da festa",
    festejando: "Festejando",
    passeando: "Passeando",
    dançando: "Dançando",
    "no-quarto-formacao": "No quarto da formação",
    "formando-fila": "Organizando fila",
    "formando-figura": "Aplicando figura",
    "organizando-formacao": "Indo à posição",
    "saindo-do-quarto": "Indo até a porta",
    desconectada: "Desconectada",
  })[status] || status;
export default function Home() {
  const [sessions, setSessions] = useState<Session[]>([]),
    [profiles, setProfiles] = useState<Profile[]>([]),
    [bots, setBots] = useState<Bot[]>([]),
    [reports, setReports] = useState<FishingReport | null>(null),
    [appearance, setAppearance] = useState<AppearanceState | null>(null),
    [roomCatalog, setRoomCatalog] = useState<RoomCatalog | null>(null),
    [view, setView] = useState<View>("overview"),
    [online, setOnline] = useState(false),
    [gearth, setGEarth] = useState<GEarthStatus>({
      running: false,
      ready: false,
      connected: false,
      state: "desligado",
    });
  const [loginOpen, setLoginOpen] = useState(false),
    [picker, setPicker] = useState<"start" | "stop" | null>(null),
    [selectedBot, setSelectedBot] = useState<"pesca" | "festa" | "formacao">("pesca"),
    [selectedRoomID, setSelectedRoomID] = useState<number | null>(null),
	[roomFilter, setRoomFilter] = useState(""),
    [destination, setDestination] = useState<FishingDestination>("infobus"),
    [fishingMode, setFishingMode] = useState<FishingMode>("manual"),
    [formationShape, setFormationShape] = useState<FormationShape>("coracao"),
    [selected, setSelected] = useState<string[]>([]),
    [selectedAppearance, setSelectedAppearance] = useState(""),
    [globalMessage, setGlobalMessage] = useState(""),
    [busy, setBusy] = useState(false),
    [reportResetting, setReportResetting] = useState(false),
    [error, setError] = useState(""),
    [notice, setNotice] = useState("");
  const [bulkLogin, setBulkLogin] = useState<BulkLoginState>({
    running: false,
		paused: false,
    total: 0,
    completed: 0,
    connected: 0,
    failed: 0,
    current: "",
  });
  const [nickname, setNickname] = useState(""),
    [mode, setMode] = useState<"steam" | "habbo">("steam"),
    [email, setEmail] = useState(""),
    [password, setPassword] = useState(""),
    [totp, setTotp] = useState(""),
    [saveLogin, setSaveLogin] = useState(true);
  const refresh = useCallback(async () => {
    try {
      const [h, s, p, b, g, r, a, rooms] = await Promise.all([
        fetch(`${api}/api/health`),
        fetch(`${api}/api/sessions`),
        fetch(`${api}/api/profiles`),
        fetch(`${api}/api/bots`),
        fetch(`${api}/api/gearth`)
          .then((response) =>
            response.ok
              ? response.json()
              : { running: false, ready: false, connected: false, state: "motor antigo" },
          )
          .catch(() => ({ running: false, ready: false, connected: false, state: "indisponível" })),
        fetch(`${reportApi}/api/reports`)
          .then((response) => (response.ok ? response.json() : null))
          .catch(() => null),
        fetch(`${api}/api/appearance`)
          .then((response) => (response.ok ? response.json() : null))
          .catch(() => null),
        fetch(`${api}/api/rooms`)
          .then((response) => (response.ok ? response.json() : null))
          .catch(() => null),
      ]);
      if (!h.ok) throw Error();
      setSessions(await s.json());
      setProfiles(await p.json());
      setBots(await b.json());
      setGEarth(g);
      if (r) setReports(r);
      if (a) {
        setAppearance(a);
        setSelectedAppearance((current) => current || a.selectedId);
      }
      if (rooms) setRoomCatalog(rooms);
      setOnline(true);
    } catch {
      setOnline(false);
    }
  }, []);
  useEffect(() => {
    const initial = setTimeout(() => void refresh(), 0);
    const t = setInterval(() => void refresh(), 1000);
    return () => {
      clearTimeout(initial);
      clearInterval(t);
    };
  }, [refresh]);
  const ready = sessions.filter((s) => connected.includes(s.status)),
    active = sessions.filter((s) => Boolean(s.automation)),
    fishingActive = active.filter((s) => s.automationId === "pesca" || !s.automationId),
    partyActive = active.filter((s) => s.automationId === "festa"),
    formationActive = active.filter((s) => s.automationId === "formacao"),
    candidates =
      picker === "stop"
        ? active.filter((s) => s.automationId === selectedBot || (selectedBot === "pesca" && !s.automationId))
        : ready.filter((s) => !s.automation);
  const profilesToConnect = profiles.filter((profile) => !profileHasSession(profile, sessions));
  const selectedRoom = roomCatalog?.rooms.find((room) => room.id === selectedRoomID);
	const visibleRooms = useMemo(() => {
		const query = roomFilter.trim().toLocaleLowerCase("pt-BR");
		if (!query) return roomCatalog?.rooms || [];
		return (roomCatalog?.rooms || []).filter((room) =>
			`${room.name} ${room.owner} ${room.description}`.toLocaleLowerCase("pt-BR").includes(query),
		);
	}, [roomCatalog, roomFilter]);
  const publicRooms = useMemo(
    () =>
      visibleRooms
        .filter((room) => Boolean(room.port && room.port > 0))
        .sort((a, b) => {
          const aReception = /recep[cç][aã]o|reception/i.test(a.name) ? 0 : 1;
          const bReception = /recep[cç][aã]o|reception/i.test(b.name) ? 0 : 1;
          if (aReception !== bReception) return aReception - bReception;
          return b.users - a.users || a.name.localeCompare(b.name, "pt-BR");
        }),
    [visibleRooms],
  );
  const playerRooms = useMemo(
    () => visibleRooms.filter((room) => !room.port || room.port <= 0),
    [visibleRooms],
  );
  const reportBySession = useMemo(
    () => new Map((reports?.accounts || []).map((account) => [account.sessionId, account])),
    [reports],
  );
  const sessionName = (session: Session) =>
    reportBySession.get(session.id)?.account || session.nickname;
  const sessionDetail = (session: Session) => {
    const reason = session.disconnectedReason || session.lastEvent || "";
    if (reason.includes("login ou senha incorretos")) {
      return `login ou senha incorretos · ${session.loginMode}`;
    }
    if (reason.includes("login recusado")) {
      return `login recusado: confira login, senha e 2FA · ${session.loginMode}`;
    }
    if (reason.includes("não concedeu permissão")) {
      return `primeiro acesso ou verificação oficial pendente · ${session.loginMode}`;
    }
    if (reason.includes("código de uso único")) {
      return `código de uso único exigido pelo Habbo · ${session.loginMode}`;
    }
    if (reason.includes("dois fatores")) {
      return `verificação em duas etapas necessária · ${session.loginMode}`;
    }
    return `${statusLabel(session.status)} · ${session.loginMode}`;
  };
  async function connectAccount(profileId?: string) {
    const authorizationWindow =
      !profileId && mode === "steam" ? window.open("about:blank", "_blank") : null;
    setBusy(true);
    setError("");
    setNotice("");
    try {
      const r = await fetch(`${api}/api/sessions`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(
          profileId
            ? { profileId }
            : { nickname, mode, email, password, totp, saveLogin },
        ),
      });
      const x = await r.json();
      if (!r.ok) throw Error(x.error);

      if (!profileId && mode === "steam") {
        const authorizationURL = `${api}/api/sessions/${x.id}/authorize`;
        if (authorizationWindow) {
          authorizationWindow.location.replace(authorizationURL);
        }
        setLoginOpen(false);
        setNotice("Autorização Steam aberta em uma sessão independente. Confirme a conta correta no navegador; você pode adicionar outras contas em seguida.");
        await refresh();
        return;
      }

      // O processo autentica de forma assíncrona. Para contas Habbo, aguarde a
      // resposta real do servidor antes de fechar o formulário; assim uma
      // credencial recusada não aparece falsamente como uma conexão concluída.
      if (profileId || mode === "habbo") {
        for (let attempt = 0; attempt < 16; attempt += 1) {
          await new Promise((resolve) => setTimeout(resolve, 500));
          const current = (await fetch(`${api}/api/sessions`).then((response) =>
            response.json(),
          )) as Session[];
          const created = current.find((session) => session.id === x.id);
          if (!created) continue;
          if (created.status === "desconectada") {
            throw Error(
              created.disconnectedReason ||
                created.lastEvent ||
                "O servidor recusou o login.",
            );
          }
          if (connected.includes(created.status)) break;
        }
      }
      setLoginOpen(false);
      setPassword("");
      setTotp("");
      setNotice("Conta conectada com sucesso.");
      await refresh();
    } catch (e) {
      authorizationWindow?.close();
      if (profileId) {
        const profile = profiles.find((item) => item.id === profileId);
        if (profile) {
          setMode("habbo");
          setNickname(profile.nickname);
          setEmail(profile.email);
          setLoginOpen(true);
        }
      }
      setError(e instanceof Error ? e.message : "Falha ao conectar");
    } finally {
      setBusy(false);
      setPassword("");
    }
  }
  async function connectAllSavedAccounts() {
    const queue = profiles.filter((profile) => !profileHasSession(profile, sessions));
    if (!queue.length) {
      setNotice("Todas as contas salvas já possuem uma sessão ativa.");
      return;
    }
    setBusy(true);
    setError("");
    setNotice("");
    setBulkLogin({
      running: true,
		paused: false,
      total: queue.length,
      completed: 0,
      connected: 0,
      failed: 0,
      current: "",
    });
    let connectedCount = 0;
    const failures: string[] = [];
    for (let index = 0; index < queue.length; index += 1) {
      const profile = queue[index];
      setBulkLogin((current) => ({
        ...current,
        current: profileName(profile),
        completed: index,
        connected: connectedCount,
        failed: failures.length,
      }));
      try {
		const waiting = sessions.find(
			(session) => session.profileId === profile.id && session.status === "aguardando-verificacao",
		);
		if (waiting) {
			await fetch(`${api}/api/sessions/${waiting.id}`, { method: "DELETE" });
		}
        const response = await fetch(`${api}/api/sessions`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ profileId: profile.id }),
        });
        const created = await response.json();
        if (!response.ok) throw Error(created.error || "não foi possível iniciar a sessão");

        let authenticated = false;
        let lastStatus = "iniciando";
        let lastReason = "";
        for (let attempt = 0; attempt < 60; attempt += 1) {
          await new Promise((resolve) => setTimeout(resolve, 500));
          const current = (await fetch(`${api}/api/sessions`).then((item) => item.json())) as Session[];
          setSessions(current);
          const session = current.find((item) => item.id === created.id);
          if (!session) continue;
          lastStatus = session.status;
          lastReason = session.disconnectedReason || session.lastEvent || "";
          if (session.status === "desconectada" || session.status === "encerrada") {
            throw Error(lastReason || "o servidor recusou o login");
          }
			if (session.status === "aguardando-verificacao") {
				setBulkLogin((current) => ({
					...current,
					running: false,
					paused: true,
					current: profileName(profile),
					completed: index,
					connected: connectedCount,
				}));
				setBusy(false);
				setNotice(
					`${profileName(profile)} exige verificação do Habbo. Conclua o acesso no cliente oficial e depois clique em “Retomar fila”.`,
				);
				await refresh();
				return;
			}
          if (connected.includes(session.status)) {
            authenticated = true;
            break;
          }
        }
        if (!authenticated) {
          throw Error(lastReason || `tempo de autenticação excedido (${statusLabel(lastStatus)})`);
        }
        connectedCount += 1;
      } catch (failure) {
        failures.push(
          `${profileName(profile)}: ${failure instanceof Error ? failure.message : "falha no login"}`,
        );
      }
      setBulkLogin((current) => ({
        ...current,
        completed: index + 1,
        connected: connectedCount,
        failed: failures.length,
      }));
      if (index < queue.length - 1) {
        await new Promise((resolve) => setTimeout(resolve, 700));
      }
    }
    setBulkLogin((current) => ({ ...current, running: false, paused: false, current: "" }));
    setBusy(false);
    setNotice(
      failures.length
        ? `Fila concluída: ${connectedCount} conectada(s) e ${failures.length} com falha.`
        : `Todas as ${connectedCount} conta(s) salvas foram conectadas.`,
    );
    if (failures.length) setError(failures.join(" · "));
    await refresh();
  }
  async function remove(url: string) {
    setBusy(true);
    setError("");
    setNotice("");
    try {
      const response = await fetch(url, { method: "DELETE" });
      if (!response.ok) throw Error("Não foi possível concluir a remoção.");
      setNotice("Operação concluída.");
      await refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Falha ao remover");
    } finally {
      setBusy(false);
    }
  }
  async function sendGlobalMessage() {
    const message = globalMessage.trim();
    if (!message) {
      setError("Escreva uma mensagem antes de enviar.");
      return;
    }
    setBusy(true);
    setError("");
    setNotice("");
    try {
      const response = await fetch(`${api}/api/chat/global`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ message }),
      });
      const result = await response.json().catch(() => ({}));
      if (!response.ok) throw Error(result.error || "Não foi possível enviar a mensagem.");
      setGlobalMessage("");
      const ignored = Number(result.ignoradas || 0);
      const failed = Object.keys((result.falhas || {}) as Record<string, string>).length;
      setNotice(
        `Mensagem enviada uma vez por ${result.enviadas?.length || 0} conta(s) no quarto.` +
          (ignored ? ` ${ignored} fora de quarto.` : "") +
          (failed ? ` ${failed} falha(s) de entrega.` : ""),
      );
    } catch (e) {
      setError(e instanceof Error ? e.message : "Falha ao enviar mensagem global");
    } finally {
      setBusy(false);
    }
  }
  async function resetReports() {
    if (!window.confirm("Zerar todas as capturas, XP e métricas dos relatórios? As sessões e automações em andamento não serão interrompidas.")) {
      return;
    }
    setReportResetting(true);
    setError("");
    setNotice("");
    try {
      const response = await fetch(`${reportApi}/api/reports/reset`, { method: "POST" });
      const result = await response.json().catch(() => null);
      if (!response.ok || !result) throw Error("Não foi possível zerar os relatórios.");
      setReports(result as FishingReport);
      setNotice("Relatórios de pesca zerados. As sessões atuais continuaram rodando normalmente.");
    } catch (e) {
      setError(e instanceof Error ? e.message : "Falha ao zerar os relatórios");
    } finally {
      setReportResetting(false);
    }
  }
  async function automate() {
    if (!picker) return;
    setBusy(true);
    setError("");
    setNotice("");
    try {
      const r = await fetch(`${api}/api/bots/${selectedBot}/${picker}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(
          picker === "start"
            ? selectedBot === "pesca"
              ? { sessionIds: selected, destination, mode: fishingMode }
              : { sessionIds: selected, roomId: selectedRoomID }
            : { sessionIds: selected },
        ),
      });
      const x = await r.json();
      const failures = Object.entries((x.falhas || {}) as Record<string, string>);
      const completed = (picker === "start" ? x.iniciadas : x.paradas) as string[] | undefined;
      if (!r.ok || !completed?.length) {
        const details = failures.map(([id, reason]) => {
          const account = sessions.find((session) => session.id === id);
          return `${account ? sessionName(account) : id}: ${reason}`;
        });
        throw Error(details.join(" · ") || x.error || "O motor não confirmou a operação.");
      }
      await refresh();
      if (failures.length) {
        setSelected(failures.map(([id]) => id));
        setError(
          `${completed.length} conta(s) concluída(s). ${failures
            .map(([id, reason]) => {
              const account = sessions.find((session) => session.id === id);
              return `${account ? sessionName(account) : id}: ${reason}`;
            })
            .join(" · ")}`,
        );
      } else {
		const distribution = (x.distribuicao || {}) as Record<string, number>;
		const distributionSummary = fishingMode === "por-nivel"
		  ? ` Infobus: ${distribution.infobus || 0} · Jardim: ${distribution["jardim-flutuante"] || 0} · Snouthill: ${distribution["snouthill-pier"] || 0}${x.semNivel ? ` · sem nível salvo: ${x.semNivel}` : ""}.`
		  : "";
        setNotice(
          picker === "start"
            ? selectedBot === "pesca"
              ? fishingMode === "por-nivel"
                ? `Pesca otimizada por nível iniciada em ${completed.length} conta(s).${distributionSummary}`
                : `Pesca iniciada em ${completed.length} conta(s) no ${fishingDestinationLabel(destination)}.`
              : selectedBot === "formacao"
                ? `Formação iniciada: ${completed.length} contas entraram e estão organizando a fila.`
                : `Festa iniciada em ${completed.length} conta(s) no quarto selecionado.`
            : `${selectedBot === "pesca" ? "Pesca" : selectedBot === "formacao" ? "Formação" : "Festa"} encerrada em ${completed.length} conta(s); todas saíram pela porta.`,
        );
        if (picker === "start" && selectedBot === "formacao") {
          setSelected(completed);
          setView("formations");
        }
        setPicker(null);
        if (!(picker === "start" && selectedBot === "formacao")) setSelected([]);
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : "Falha na automação");
    } finally {
      setBusy(false);
    }
  }
  async function applyFormationShape(shape: FormationShape) {
    const targets = formationActive.map((session) => session.id);
    if (!targets.length) {
      setError("Inicie uma formação antes de escolher a figura.");
      return;
    }
    setBusy(true);
    setError("");
    setNotice("");
    try {
      const response = await fetch(`${api}/api/bots/formacao/shape`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ sessionIds: targets, shape }),
      });
      const result = await response.json().catch(() => ({}));
      if (!response.ok) throw Error(result.error || "Não foi possível aplicar a figura.");
      setFormationShape(shape);
      setNotice(`${formationShapes.find((item) => item.id === shape)?.name || "Figura"} enviada para ${result.aplicadas?.length || 0} contas.`);
      await refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Falha ao aplicar a formação");
    } finally {
      setBusy(false);
    }
  }
  async function refreshRooms() {
    setBusy(true);
    setError("");
    setNotice("");
    try {
      const response = await fetch(`${api}/api/rooms/refresh`, { method: "POST" });
      const result = await response.json().catch(() => ({}));
      if (!response.ok) throw Error(result.error || "Não foi possível consultar os quartos.");
      setNotice("Consultando os quartos ocupados pelo navegador do hotel…");
      for (let attempt = 0; attempt < 24; attempt += 1) {
        await new Promise((resolve) => setTimeout(resolve, 500));
        const catalog = await fetch(`${api}/api/rooms`).then((item) => item.json()) as RoomCatalog;
        setRoomCatalog(catalog);
        if (!catalog.refreshing && catalog.updatedAt) {
          setNotice(`${catalog.rooms.length} quarto(s) mapeado(s), ordenados por ocupação.`);
          return;
        }
      }
      throw Error("O navegador do hotel demorou para responder.");
    } catch (e) {
      setError(e instanceof Error ? e.message : "Falha ao atualizar os quartos");
    } finally {
      setBusy(false);
    }
  }
  async function searchRoomsByOwner() {
    const owner = roomFilter.trim();
    if (!owner) {
      setError("Digite o nome do Habbo que possui o quarto.");
      return;
    }
    setBusy(true);
    setError("");
    setNotice("");
    try {
      const response = await fetch(`${api}/api/rooms/search-owner`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ owner }),
      });
      const result = await response.json().catch(() => ({}));
      if (!response.ok) throw Error(result.error || "Não foi possível buscar os quartos desse Habbo.");
      setNotice(`Buscando os quartos de ${owner} no navegador do hotel…`);
      for (let attempt = 0; attempt < 12; attempt += 1) {
        await new Promise((resolve) => setTimeout(resolve, 500));
        const catalog = await fetch(`${api}/api/rooms`).then((item) => item.json()) as RoomCatalog;
        setRoomCatalog(catalog);
        if (!catalog.refreshing && catalog.updatedAt) {
          setNotice(catalog.rooms.length
            ? `${catalog.rooms.length} quarto(s) encontrado(s) para ${owner}.`
            : `Nenhum quarto encontrado para ${owner}.`);
          return;
        }
      }
      throw Error("O navegador do hotel demorou para responder.");
    } catch (e) {
      setError(e instanceof Error ? e.message : "Falha ao buscar os quartos");
    } finally {
      setBusy(false);
    }
  }
  async function applyAppearance() {
    if (!selectedAppearance) return;
    setBusy(true);
    setError("");
    setNotice("");
    try {
      const response = await fetch(`${api}/api/appearance/apply`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ presetId: selectedAppearance }),
      });
      const result = await response.json().catch(() => ({}));
      if (!response.ok) {
        throw Error(result.error || "Não foi possível aplicar o visual.");
      }
      await refresh();
      const failures = Object.keys(result.failures || {}).length;
      setNotice(
        failures
          ? `Visual salvo como padrão e aplicado em ${result.applied} conta(s); ${failures} conta(s) não responderam.`
          : result.online
            ? `Visual aplicado nas ${result.applied} conta(s) online e salvo para os próximos logins.`
            : "Visual salvo como padrão para todas as próximas contas.",
      );
    } catch (e) {
      setError(e instanceof Error ? e.message : "Falha ao aplicar o visual");
    } finally {
      setBusy(false);
    }
  }
  const headings = {
    overview: ["Central de operações", "Visão geral"],
    accounts: ["Acesso rápido", "Contas"],
    automations: ["Controle por conta", "Automações"],
    rooms: ["Navegador do hotel", "Quartos movimentados"],
    formations: ["Posições coordenadas", "Formações"],
    appearance: ["Identidade global", "Skins"],
    reports: ["Histórico ao vivo", "Relatórios"],
  };
  return (
    <main className="app-shell command-surface">
      <header className="app-header">
        <div className="brand-lockup">
          <span className="brand-mark" aria-label="Habbo Headless">
            <Bot size={21} strokeWidth={2.25} />
          </span>
          <div>
            <p className="brand-kicker">Painel local</p>
            <h1>Habbo Headless</h1>
          </div>
        </div>
        <div className="header-center" aria-label="Status do motor">
          <span className={online ? "header-signal online" : "header-signal"} />
          <span>{online ? "Motor conectado" : "Motor offline"}</span>
        </div>
        <div className="topbar-status">
          <GEarthStatusPill status={gearth} />
          <Status online={online} />
        </div>
      </header>
      <div className="app-canvas">
        <nav className="command-tabs" aria-label="Navegação principal">
          <div className="command-tabs-inner">
            {(
              [
                ["overview", "Visão geral", LayoutDashboard],
                ["accounts", "Contas", UsersRound],
                ["automations", "Automações", Zap],
                ["rooms", "Quartos", MapPinned],
                ["formations", "Formações", Shapes],
                ["appearance", "Visuais", Palette],
                ["reports", "Relatórios", BarChart3],
              ] as [View, string, typeof LayoutDashboard][]
            ).map(([id, label, Icon]) => (
              <button
                key={id}
                aria-current={view === id ? "page" : undefined}
                onClick={() => setView(id)}
                className={`tab-entry ${view === id ? "active" : ""}`}
              >
                <Icon size={20} strokeWidth={2} />
                <span>{label}</span>
              </button>
            ))}
          </div>
        </nav>
        <section className="workspace command-workspace">
          <div className="workspace-heading">
            <div className="heading-copy">
              <p className="section-kicker">{headings[view][0]}</p>
              <div className="heading-title-row"><h2>{headings[view][1]}</h2></div>
            </div>
            <div className="heading-context">
              <Radio size={15} />
              <span>{ready.length} conta(s) prontas</span>
            </div>
            {view === "accounts" && (
              <div className="workspace-actions">
                <button
                  onClick={() => {
                    setError("");
                    setLoginOpen(true);
                  }}
                  className="primary"
                >
                  <LogIn size={16} />
                  Nova conta
                </button>
              </div>
            )}
          </div>
          <div aria-live="polite">
            {notice && <p className="notice">{notice}</p>}
            {!picker && !loginOpen && error && <p className="error banner-error">{error}</p>}
          </div>
          {view === "overview" && (
            <>
              <div className="mt-6 grid gap-4 sm:grid-cols-2 xl:grid-cols-5">
                <Metric label="Conectadas" value={ready.length} />
                <Metric label="Pescando" value={fishingActive.length} />
                <Metric label="Em festa" value={partyActive.length} />
                <Metric label="Em formação" value={formationActive.length} />
                <Metric
                  label="Execuções"
                  value={sessions.reduce((n, s) => n + s.automationStarts, 0)}
                />
              </div>
              <div className="hero">
                <div className="flex flex-wrap items-center justify-between gap-4">
                  <div>
                    <p className="eyebrow">Transporte de protocolo</p>
                    <h3 className="mt-2 text-xl font-bold">Codex G-Earth integrado</h3>
                    <p className="mt-2 text-sm text-slate-400">
                      {gearth.state}. O núcleo inicia sozinho para cada conta, sem janela do G-Earth, Habbo Launcher ou motor gráfico.
                    </p>
                  </div>
                  <span className="badge">{gearth.instances ?? 0} instância(s)</span>
                </div>
                {error && <p className="error">{error}</p>}
              </div>
              <div className="hero">
                <p className="text-sm text-slate-400">Operação atual</p>
                <h3 className="mt-2 text-2xl font-bold">
                  {active.length
                    ? `${active.length} conta(s) em automação agora`
                    : "Nenhuma automação em execução"}
                </h3>
                <p className="mt-2 text-sm text-slate-400">
                  Inicie e pare cada automação nas contas que você escolher.
                </p>
              </div>
              <div className="hero">
                <div className="flex flex-wrap items-end justify-between gap-4">
                  <div>
                    <p className="eyebrow">Chat global</p>
                    <h3 className="mt-2 text-xl font-bold">Gritar em todos os quartos ativos</h3>
                    <p className="mt-2 text-sm text-slate-400">
                      Cada conta que estiver dentro de um quarto gritará o texto uma única vez, para todos verem, sem interromper a automação.
                    </p>
                  </div>
                  <span className="badge">{ready.filter((session) => ["no-quarto-pesca", "aguardando-peixe", "indo-pescar", "vara-lançada", "no-quarto-festa", "festejando", "passeando", "dançando"].includes(session.status)).length} no quarto</span>
                </div>
                <form
                  className="mt-4 flex flex-col gap-3 sm:flex-row"
                  onSubmit={(event) => {
                    event.preventDefault();
                    void sendGlobalMessage();
                  }}
                >
                  <input
                    value={globalMessage}
                    maxLength={100}
                    onChange={(event) => setGlobalMessage(event.target.value)}
                    placeholder="Escreva a mensagem que os bots vão gritar…"
                    aria-label="Mensagem global para gritar no hotel"
                    className="input flex-1"
                  />
                  <button className="primary" disabled={busy || !globalMessage.trim()} type="submit">
                    Gritar para todos
                  </button>
                </form>
                <p className="mt-2 text-right text-xs text-slate-500">{globalMessage.length}/100</p>
              </div>
            </>
          )}
          {view === "accounts" && (
            <div className="mt-6 grid gap-5 xl:grid-cols-2">
              <Panel
                title="Logins salvos"
                sub="Escolha conexão individual ou em lote"
              >
                <div className="connection-mode-card">
                  <div>
                    <p className="eyebrow">Como deseja entrar?</p>
                    <h3>Você controla o ritmo</h3>
                    <p>Conecte uma conta agora ou avance por todas as contas salvas em fila.</p>
                  </div>
                  <div className="connection-mode-actions">
                    <button
                      className="primary"
                      onClick={() => {
                        setError("");
                        setLoginOpen(true);
                      }}
                    >
                      Entrar em uma conta
                    </button>
                    <button
                      className="secondary"
                      disabled={busy || profilesToConnect.length === 0}
                      onClick={() => void connectAllSavedAccounts()}
                    >
                      {bulkLogin.paused ? "Retomar todas" : "Conectar todas"}
                    </button>
                  </div>
                </div>
                {profiles.length > 0 && (
                  <div className="mb-4 rounded-2xl border border-cyan-400/20 bg-cyan-400/[.06] p-4">
                    <div className="flex flex-wrap items-center justify-between gap-3">
                      <div>
                        <p className="font-bold">Conexão em fila</p>
                        <p className="text-xs text-slate-400">
                          {bulkLogin.paused
                            ? `${bulkLogin.current} aguarda verificação; a fila está pausada`
                            : bulkLogin.running
                              ? `Conectando ${bulkLogin.current} · ${bulkLogin.completed}/${bulkLogin.total}`
                              : profilesToConnect.length
                                ? `${profilesToConnect.length} conta(s) salva(s) ainda sem sessão`
                                : "Todas as contas salvas já estão na lista de sessões"}
                        </p>
                      </div>
                      <button
                        className="primary"
                        disabled={busy || profilesToConnect.length === 0}
                        onClick={() => void connectAllSavedAccounts()}
                      >
						{bulkLogin.running ? "Conectando…" : bulkLogin.paused ? "Retomar fila" : "Conectar todas"}
                      </button>
                    </div>
                    {bulkLogin.running && (
                      <div className="mt-3" aria-live="polite">
                        <div className="h-2 overflow-hidden rounded-full bg-slate-800">
                          <div
                            className="h-full rounded-full bg-cyan-400 transition-all"
                            style={{
                              width: `${bulkLogin.total ? (bulkLogin.completed / bulkLogin.total) * 100 : 0}%`,
                            }}
                          />
                        </div>
                        <p className="mt-2 text-xs text-slate-400">
                          {bulkLogin.connected} conectada(s) · {bulkLogin.failed} falha(s)
                        </p>
                      </div>
                    )}
					{bulkLogin.paused && (
						<p className="mt-3 rounded-xl border border-amber-400/30 bg-amber-400/10 p-3 text-xs leading-5 text-amber-100">
							A conta atual aguarda a verificação oficial do Habbo. Após confirmar esse acesso, retome a fila para continuar pelas demais contas.
						</p>
					)}
                  </div>
                )}
                {profiles.length ? (
                  profiles.map((p) => (
                    <Row
                      key={p.id}
                      title={profileName(p)}
                      sub={p.email}
                    >
                      <button
                        disabled={busy}
                        onClick={() => void connectAccount(p.id)}
                        className="primary small"
                      >
                        Conectar
                      </button>
                      <button
                        onClick={() =>
                          void remove(`${api}/api/profiles/${p.id}`)
                        }
                        aria-label={`Excluir ${profileName(p)}`}
                        className="text-red-300"
                      >
                        ×
                      </button>
                    </Row>
                  ))
                ) : (
                  <Empty text="Nenhum login Habbo salvo." />
                )}
              </Panel>
              <Panel
                title="Sessões atuais"
                sub={`${sessions.length} sessão(ões)`}
              >
                {sessions.length ? (
                  sessions.map((s) => (
                    <Row
                      key={s.id}
                      title={sessionName(s)}
                      sub={sessionDetail(s)}
                    >
                      <button
                        onClick={() =>
                          void remove(`${api}/api/sessions/${s.id}`)
                        }
                        className="danger small"
                      >
                        Desconectar
                      </button>
						{s.status === "aguardando-verificacao" && (
							<span className="badge border-amber-400/40 text-amber-200">Verificação pendente</span>
						)}
                      {s.loginMode === "steam" &&
                        s.status === "aguardando-autorização" && (
                          <a
                            href={`${api}/api/sessions/${s.id}/authorize`}
                            target="_blank"
                            rel="noreferrer"
                            className="primary small"
                          >
                            Autorizar Steam
                          </a>
                        )}
                    </Row>
                  ))
                ) : (
                  <Empty text="Nenhuma conta conectada." />
                )}
              </Panel>
            </div>
          )}
          {view === "automations" && (
            <div className="mt-6 grid gap-5 lg:grid-cols-[1.2fr_.8fr]">
              {bots.map((b) => (
                <article key={b.id} className="hero mt-0">
                  <div className="flex justify-between gap-3">
                    <div>
                      <p className="eyebrow">{b.mode}</p>
                      <h3 className="mt-2 text-2xl font-black">{b.name}</h3>
                    </div>
                    <span className="badge">
                      {(b.id === "pesca" ? fishingActive : b.id === "formacao" ? formationActive : partyActive).length} ativa(s)
                    </span>
                  </div>
                  <p className="mt-4 text-sm leading-6 text-slate-400">
                    {b.description} Escolha uma, várias ou todas as contas.
                  </p>
                  <div className="mt-7 flex gap-3">
                    <button
                      disabled={!ready.some((s) => !s.automation)}
                      onClick={() => {
                        setError("");
                        setSelectedBot(b.id as "pesca" | "festa" | "formacao");
                        if (b.id === "festa" || b.id === "formacao") {
                          setView("rooms");
                          return;
                        }
                        setSelected(ready.filter((s) => !s.automation).map((s) => s.id));
                        setPicker("start");
                      }}
                      className="primary"
                    >
                      {b.id === "festa" || b.id === "formacao" ? "Escolher quarto" : "▶ Iniciar"}
                    </button>
                    <button
                      disabled={!(b.id === "pesca" ? fishingActive : b.id === "formacao" ? formationActive : partyActive).length}
                      onClick={() => {
                        setError("");
                        setSelectedBot(b.id as "pesca" | "festa" | "formacao");
                        setSelected((b.id === "pesca" ? fishingActive : b.id === "formacao" ? formationActive : partyActive).map((s) => s.id));
                        setPicker("stop");
                      }}
                      className="danger"
                    >
                      ■ Parar
                    </button>
                  </div>
                </article>
              ))}
              <Panel title="Em execução" sub="Atualização em tempo real">
                {active.length ? (
                  active.map((s) => (
                    <AutomationRow
                      key={s.id}
                      session={s}
                      name={sessionName(s)}
                      report={reportBySession.get(s.id)}
                      observedAt={reports?.updatedAt}
                      busy={busy}
                      stop={() => {
                        setError("");
                        setSelectedBot((s.automationId || "pesca") as "pesca" | "festa" | "formacao");
                        setSelected([s.id]);
                        setPicker("stop");
                      }}
                    />
                  ))
                ) : (
                  <Empty text="Nenhuma automação ativa." />
                )}
              </Panel>
            </div>
          )}
          {view === "rooms" && (
            <div className="mt-6 space-y-5">
              <div className="hero mt-0">
                <div className="flex flex-wrap items-start justify-between gap-4">
                  <div className="max-w-2xl">
                    <p className="eyebrow">{selectedBot === "formacao" ? "Automação Formações" : "Automação Passeio e festa"}</p>
                    <h3 className="mt-2 text-2xl font-black">{selectedBot === "formacao" ? "Escolha a sala para a formação" : "Escolha onde concentrar as contas"}</h3>
                    <p className="mt-2 text-sm leading-6 text-slate-400">
                      {selectedBot === "formacao"
                        ? "As 35 contas entram no quarto, ocupam uma fila organizada e aguardam a figura ou palavra escolhida."
                        : "Escolha um quarto público do hotel ou um quarto de jogador. Depois de entrar, as contas caminham e dançam continuamente."}
                    </p>
                  </div>
                  <button
                    className="primary"
                    disabled={busy || !ready.some((session) => !session.automation)}
                    onClick={() => void refreshRooms()}
                  >
                    {roomCatalog?.refreshing ? "Consultando…" : "Atualizar quartos"}
                  </button>
                </div>
                <p className="mt-4 text-xs text-slate-500">
                  {roomCatalog?.updatedAt
                    ? `Última leitura ${when(roomCatalog.updatedAt)}`
                    : "Faça a primeira leitura usando uma conta conectada e livre."}
                </p>
				<div className="mt-4 flex flex-col gap-2 sm:flex-row">
					<input
						value={roomFilter}
						onChange={(event) => setRoomFilter(event.target.value)}
						onKeyDown={(event) => {
							if (event.key === "Enter") void searchRoomsByOwner();
						}}
					placeholder="Nome do quarto, Habbo ou descrição"
					aria-label="Filtrar quartos pelo nome, Habbo ou descrição"
						className="input min-w-0 flex-1"
					/>
					<button className="secondary shrink-0" disabled={busy || !roomFilter.trim()} onClick={() => void searchRoomsByOwner()}>
					Buscar no hotel
					</button>
				</div>
				<p className="mt-2 text-xs text-slate-500">
					A lista traz destinos públicos e quartos de Habbos. A busca do hotel procura pelo texto informado; o filtro também encontra título, dono e descrição já mapeados.
				</p>
              </div>
			  {visibleRooms.length ? (
                <div className="space-y-5">
                  {publicRooms.length > 0 && (
                    <section>
                      <div className="mb-2 flex items-center gap-2 px-1">
                        <span className="badge">Quartos públicos</span>
                        <span className="text-xs text-slate-500">Destinos oficiais do hotel, incluindo a Recepção quando disponível.</span>
                      </div>
                      <div className="room-list">
				  {publicRooms.map((room, index) => {
                    const isOpen = room.access === "open";
                    const selectedRoom = selectedRoomID === room.id;
                    return (
                      <button
                        key={room.id}
                        type="button"
                        disabled={!isOpen}
                        aria-pressed={selectedRoom}
                        onClick={() => setSelectedRoomID(room.id)}
                        className={`room-card ${selectedRoom ? "selected" : ""}`}
                      >
                        <span className="room-rank">#{index + 1}</span>
                        <span className="min-w-0 flex-1">
                          <span className="flex flex-wrap items-center gap-2">
                            <b className="truncate">{room.name}</b>
                            <em>{isOpen ? "aberto" : room.access}</em>
                          </span>
                          <small>quarto público · ID {room.id}</small>
                          {room.description && <p>{room.description}</p>}
                        </span>
                        <span className="room-occupancy">
                          <strong>{room.users}</strong>
                          <small>de {room.capacity}</small>
                        </span>
                      </button>
                    );
                  })}
                      </div>
                    </section>
                  )}
                  {playerRooms.length > 0 && (
                    <section>
                      <div className="mb-2 flex items-center gap-2 px-1">
                        <span className="badge">Quartos de Habbos</span>
                        <span className="text-xs text-slate-500">Salas de jogadores abertas no navegador.</span>
                      </div>
                      <div className="room-list">
				  {playerRooms.map((room, index) => {
                    const isOpen = room.access === "open";
                    const selectedRoom = selectedRoomID === room.id;
                    return (
                      <button
                        key={room.id}
                        type="button"
                        disabled={!isOpen}
                        aria-pressed={selectedRoom}
                        onClick={() => setSelectedRoomID(room.id)}
                        className={`room-card ${selectedRoom ? "selected" : ""}`}
                      >
                        <span className="room-rank">#{index + 1}</span>
                        <span className="min-w-0 flex-1">
                          <span className="flex flex-wrap items-center gap-2">
                            <b className="truncate">{room.name}</b>
                            <em>{isOpen ? "aberto" : room.access}</em>
                          </span>
                          <small>por {room.owner || "Habbo"} · ID {room.id}</small>
                          {room.description && <p>{room.description}</p>}
                        </span>
                        <span className="room-occupancy">
                          <strong>{room.users}</strong>
                          <small>de {room.capacity}</small>
                        </span>
                      </button>
                    );
                  })}
                      </div>
                    </section>
                  )}
                </div>
              ) : (
                <Empty text="Nenhum quarto mapeado ainda." />
              )}
              {selectedRoom && (
                <div className="skin-action">
                  <div>
                    <b>{selectedRoom.name}</b>
                    <p>{selectedRoom.users} pessoa(s) agora · {selectedBot === "formacao" ? "as contas entrarão e formarão uma fila organizada." : "os bots entrarão, andarão e dançarão."}</p>
                  </div>
                  <button
                    className="primary"
                    disabled={busy || !ready.some((session) => !session.automation)}
                    onClick={() => {
                      const available = ready.filter((session) => !session.automation).map((session) => session.id);
                      if (selectedBot === "formacao") {
                        setSelected(available.slice(0, 35));
                      } else {
                        setSelectedBot("festa");
                        setSelected(available);
                      }
                      setPicker("start");
                    }}
                  >
                    {selectedBot === "formacao" ? "Selecionar 35 contas" : "Escolher contas"}
                  </button>
                </div>
              )}
            </div>
          )}
          {view === "formations" && (
            <div className="mt-6 space-y-5">
              <div className="hero mt-0">
                <p className="eyebrow">Formações com 35 contas</p>
                <h3 className="mt-2 text-2xl font-black">Fila pronta? Escolha a figura</h3>
                <p className="mt-2 max-w-2xl text-sm leading-6 text-slate-400">
                  A formação mantém cada conta na posição dela. Ao trocar a seleção, todas caminham até a nova coordenada; parar faz cada uma sair pela porta.
                </p>
                <div className="mt-5 flex flex-wrap items-center gap-3">
                  <span className="badge">{formationActive.length}/35 contas em formação</span>
                  <button
                    className="danger small"
                    disabled={busy || !formationActive.length}
                    onClick={() => {
                      setSelectedBot("formacao");
                      setSelected(formationActive.map((session) => session.id));
                      setPicker("stop");
                    }}
                  >
                    ■ Parar e sair do quarto
                  </button>
                </div>
              </div>
              <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
                {formationShapes.map((shape) => (
                  <button
                    key={shape.id}
                    type="button"
                    disabled={busy || formationActive.length !== 35}
                    onClick={() => void applyFormationShape(shape.id)}
                    className={`skin-card text-left ${formationShape === shape.id ? "selected" : ""}`}
                  >
                    <span className="text-3xl leading-none">{shape.preview}</span>
                    <b className="mt-3 block">{shape.name}</b>
                    <small className="mt-1 block leading-5">{shape.description}</small>
                  </button>
                ))}
              </div>
              {formationActive.length !== 35 && (
                <Empty text="A figura será liberada quando as 35 contas confirmarem a formação no quarto." />
              )}
            </div>
          )}
          {view === "appearance" && (
            <div className="mt-6">
              <div className="hero mt-0">
                <div className="flex flex-wrap items-start justify-between gap-4">
                  <div className="max-w-2xl">
                    <p className="eyebrow">Mod global de aparência</p>
                    <h3 className="mt-2 text-2xl font-black">Um visual para toda a operação</h3>
                    <p className="mt-2 text-sm leading-6 text-slate-400">
                      A troca alcança todas as contas conectadas e fica salva como padrão. Toda conta adicionada depois já entra usando o visual escolhido.
                    </p>
                  </div>
                  <span className="badge">{ready.length} conta(s) online</span>
                </div>
              </div>
              {appearance ? (
                <>
                  <div className="skin-grid">
                    {appearance.presets.map((preset) => {
                      const selectedSkin = selectedAppearance === preset.id;
                      const activeSkin = appearance.selectedId === preset.id;
                      return (
                        <button
                          key={preset.id}
                          type="button"
                          aria-pressed={selectedSkin}
                          onClick={() => setSelectedAppearance(preset.id)}
                          className={`skin-card ${selectedSkin ? "selected" : ""}`}
                        >
                          <span className="skin-preview">
                            {preset.preview ? (
                              <img src={preset.preview} alt={`Prévia do visual ${preset.name}`} />
                            ) : (
                              <span className="skin-symbol" aria-hidden="true">🎣</span>
                            )}
                          </span>
                          <span className="skin-copy">
                            <span className="flex items-center justify-between gap-2">
                              <b>{preset.name}</b>
                              {activeSkin && <em>Em uso</em>}
                            </span>
                            <small>{preset.description}</small>
                          </span>
                        </button>
                      );
                    })}
                  </div>
                  <div className="skin-action">
                    <div>
                      <b>{appearance.presets.find((item) => item.id === selectedAppearance)?.name}</b>
                      <p>Aplicar agora em todas e usar nos próximos logins.</p>
                    </div>
                    <button
                      className="primary"
                      disabled={busy || !selectedAppearance}
                      onClick={() => void applyAppearance()}
                    >
                      {busy ? "Aplicando…" : "Aplicar em todas"}
                    </button>
                  </div>
                </>
              ) : (
                <Empty text="O módulo de skins ficará disponível após a próxima inicialização do motor atualizado." />
              )}
            </div>
          )}
          {view === "reports" && (
            <FishingReports reports={reports} onReset={resetReports} resetting={reportResetting} />
          )}
        </section>
      </div>
      {picker && (
        <Modal
          title={`${picker === "start" ? "Iniciar" : "Parar"} ${selectedBot === "pesca" ? "pesca" : selectedBot === "formacao" ? "formação" : "festa"}`}
          close={() => setPicker(null)}
        >
          {picker === "start" && selectedBot === "pesca" && (
            <>
              <fieldset className="destination-fieldset">
                <legend>Como distribuir a pesca?</legend>
                <div className="destination-grid">
                  <label className={`destination-card ${fishingMode === "manual" ? "selected" : ""}`}>
                    <input
                      type="radio"
                      name="fishing-mode"
                      checked={fishingMode === "manual"}
                      onChange={() => setFishingMode("manual")}
                    />
                    <span>
                      <b>Tudo na mesma sala</b>
                      <small>Escolha manual</small>
                      <em>Todas as contas pescam no destino selecionado abaixo.</em>
                    </span>
                  </label>
                  <label className={`destination-card ${fishingMode === "por-nivel" ? "selected" : ""}`}>
                    <input
                      type="radio"
                      name="fishing-mode"
                      checked={fishingMode === "por-nivel"}
                      onChange={() => setFishingMode("por-nivel")}
                    />
                    <span>
                      <b>Pesca otimizada por nível</b>
                      <small>Distribuição automática</small>
                      <em>1–29: Infobus · 30–69: Jardim · 70+: Snouthill.</em>
                    </span>
                  </label>
                </div>
              </fieldset>
              {fishingMode === "manual" ? (
                <fieldset className="destination-fieldset">
                  <legend>Onde os bots devem pescar?</legend>
                  <div className="destination-grid">
                    {fishingDestinations.map((room) => (
                      <label
                        key={room.id}
                        className={`destination-card ${destination === room.id ? "selected" : ""}`}
                      >
                        <input
                          type="radio"
                          name="fishing-destination"
                          value={room.id}
                          checked={destination === room.id}
                          onChange={() => setDestination(room.id)}
                        />
                        <span>
                          <b>{room.name}</b>
                          <small>{room.level}</small>
                          <em>{room.description}</em>
                        </span>
                      </label>
                    ))}
                  </div>
                </fieldset>
              ) : (
                <p className="muted">Contas sem nível salvo vão para o Infobus até o motor confirmar a leitura do nível.</p>
              )}
            </>
          )}
          {picker === "start" && selectedBot === "festa" && selectedRoom && (
            <div className="selected-room-summary">
              <span>
                <small>Quarto selecionado</small>
                <b>{selectedRoom.name}</b>
              </span>
              <strong>{selectedRoom.users}/{selectedRoom.capacity}</strong>
            </div>
          )}
          {picker === "start" && selectedBot === "formacao" && selectedRoom && (
            <div className="selected-room-summary">
              <span>
                <small>Fluxo da formação</small>
                <b>{selectedRoom.name} · fila primeiro, figura depois</b>
              </span>
              <strong>{selected.length}/35</strong>
            </div>
          )}
          <button
            onClick={() =>
              setSelected(
                selected.length === candidates.length
                  ? []
                  : candidates.map((s) => s.id),
              )
            }
            className="text-sm font-bold text-cyan-300"
          >
            {selected.length === candidates.length
              ? "Desmarcar todas"
              : "Selecionar todas"}
          </button>
          <div className="mt-3 space-y-2">
            {candidates.map((s) => (
              <label key={s.id} className="choice">
                <input
                  type="checkbox"
                  checked={selected.includes(s.id)}
                  onChange={() =>
                    setSelected((v) =>
                      v.includes(s.id)
                        ? v.filter((id) => id !== s.id)
                        : [...v, s.id],
                    )
                  }
                />
                <span>
                  <b className="block">{sessionName(s)}</b>
                  <small className="text-slate-500">{s.status}</small>
                </span>
              </label>
            ))}
          </div>
          {error && <p className="error">{error}</p>}
          <button
            disabled={busy || !selected.length || (picker === "start" && selectedBot === "formacao" && selected.length !== 35)}
            onClick={() => void automate()}
            className={picker === "start" ? "primary full" : "danger full"}
          >
            {busy
              ? "Processando…"
              : picker === "start" && selectedBot === "formacao" && selected.length !== 35
                ? `Selecione 35 contas (${selected.length}/35)`
                : `${picker === "start" ? "Iniciar" : "Parar"} em ${selected.length} conta(s)`}
          </button>
        </Modal>
      )}
      {loginOpen && (
        <Modal title="Adicionar conta" close={() => setLoginOpen(false)}>
          <div className="tabs">
            <button
              onClick={() => setMode("steam")}
              className={mode === "steam" ? "active" : ""}
            >
              Steam
            </button>
            <button
              onClick={() => setMode("habbo")}
              className={mode === "habbo" ? "active" : ""}
            >
              Conta Habbo
            </button>
          </div>
          <div className="mt-4 space-y-3">
            <Input
              value={nickname}
              set={setNickname}
              placeholder="Apelido temporário (opcional)"
            />
            {mode === "habbo" && (
              <>
                <Input
                  value={email}
                  set={setEmail}
                  placeholder="E-mail ou nome de usuário"
                />
                <Input
                  value={password}
                  set={setPassword}
                  placeholder="Senha"
                  type="password"
                />
                <Input
                  value={totp}
                  set={setTotp}
                  placeholder="Código 2FA (opcional)"
                />
                <label className="choice">
                  <input
                    type="checkbox"
                    checked={saveLogin}
                    onChange={(e) => setSaveLogin(e.target.checked)}
                  />
                  Salvar login protegido neste Windows
                </label>
              </>
            )}
          </div>
          <p className="mt-3 text-xs leading-5 text-slate-500">
            {mode === "steam"
              ? "Cada conta Steam cria uma sessão independente. Na página oficial, confira o usuário exibido ou use a opção de trocar de conta antes de autorizar."
              : "A senha é criptografada para seu usuário do Windows e não retorna ao painel."}
          </p>
          {error && <p className="error">{error}</p>}
          <button
            disabled={
              busy ||
              (mode === "habbo" && (!email || !password))
            }
            onClick={() => void connectAccount()}
            className="primary full"
          >
            {busy ? "Conectando…" : "Conectar conta"}
          </button>
        </Modal>
      )}
    </main>
  );
}
function AutomationRow({
  session,
  name,
  report,
  observedAt,
  busy,
  stop,
}: {
  session: Session;
  name: string;
  report?: AccountReport;
  observedAt?: string;
  busy: boolean;
  stop: () => void;
}) {
  const isFishing = session.automationId === "pesca" || !session.automationId;
  const captureAge = report?.lastCaptureAt && observedAt
    ? new Date(observedAt).getTime() - new Date(report.lastCaptureAt).getTime()
    : Number.POSITIVE_INFINITY;
  const isStopping = session.automation === "parando";
  const currentRunSeconds = session.automationStartedAt && observedAt
    ? Math.max(
        0,
        Math.floor(
          (new Date(observedAt).getTime() - new Date(session.automationStartedAt).getTime()) /
            1000,
        ),
      )
    : 0;
  const healthy = !isFishing || captureAge < 90_000 || currentRunSeconds < 90;
  const healthText = isStopping
    ? "Confirmando parada"
    : healthy
      ? "Atividade normal"
      : "Sem captura recente";
  return (
    <article className="automation-row">
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <strong className="truncate">{name}</strong>
          <span className={`activity ${isStopping ? "stopping" : healthy ? "healthy" : "warning"}`}>
            <span /> {healthText}
          </span>
        </div>
        <p className="mt-1 text-xs text-slate-400">
          {isFishing
            ? `${statusLabel(session.status)} · ${session.fishingRoom || fishingDestinationLabel(session.fishingDestination)} · ${report?.captures ?? 0} captura(s) · ${report?.xp ?? 0} XP`
            : `${statusLabel(session.status)} · ${session.automationTarget || "quarto selecionado"}`}
        </p>
        <p className="mt-1 text-[11px] text-slate-600">
          {isFishing ? `Última captura ${when(report?.lastCaptureAt)} · ` : ""}execução atual {duration(currentRunSeconds)}
        </p>
      </div>
      <button
        disabled={busy || isStopping}
        onClick={stop}
        className="danger small shrink-0"
      >
        {isStopping ? "Parando…" : "Parar"}
      </button>
    </article>
  );
}
function FishingReports({
  reports,
  onReset,
  resetting,
}: {
  reports: FishingReport | null;
  onReset: () => void;
  resetting: boolean;
}) {
  const allAccounts = reports?.accounts || [];
  const activeAccounts = allAccounts.filter((account) => account.currentlyFishing);
  const meaningfulAccounts = allAccounts.filter(
    (account) => account.currentlyFishing || account.captures > 0,
  );
  const displayedAccounts = activeAccounts.length ? activeAccounts : meaningfulAccounts;
  const [selectedAccountId, setSelectedAccountId] = useState("");
  const [selectedHistory, setSelectedHistory] = useState<Capture[]>([]);
  const [reportQuery, setReportQuery] = useState("");
  const [reportFilter, setReportFilter] = useState<"all" | "below-goal" | "alerts">("all");
  const selectedAccount = displayedAccounts.find((account) => reportAccountKey(account) === selectedAccountId) || displayedAccounts[0];
  const selectedAccountHistoryId = selectedAccount ? reportAccountKey(selectedAccount) : "";
  const visibleAccounts = (() => {
    const query = reportQuery.trim().toLocaleLowerCase("pt-BR");
    return displayedAccounts.filter((account) => {
      if (query && !account.account.toLocaleLowerCase("pt-BR").includes(query)) return false;
      if (reportFilter === "below-goal") return account.fishPerMinute < 2;
      if (reportFilter === "alerts") return account.timeouts > 0 || account.routesBlocked > 0 || account.targetContentions > 0;
      return true;
    });
  })();

  useEffect(() => {
    if (!selectedAccountHistoryId) return;
    let cancelled = false;
    fetch(`${reportApi}/api/reports/accounts/${encodeURIComponent(selectedAccountHistoryId)}/history`)
      .then((response) => (response.ok ? response.json() : []))
      .then((history: Capture[]) => {
        if (!cancelled) setSelectedHistory(Array.isArray(history) ? history : []);
      })
      .catch(() => {
        if (!cancelled) setSelectedHistory([]);
      });
    return () => {
      cancelled = true;
    };
  }, [selectedAccountHistoryId]);

  if (!reports) {
    return (
      <div className="mt-6 card">
        <Empty text="O coletor de pesca está iniciando. Os dados aparecerão aqui sem parar as sessões." />
      </div>
    );
  }
  const displayedAccountIds = new Set(displayedAccounts.map((account) => reportAccountKey(account)));
  const displayedCaptures = activeAccounts.length
    ? reports.captures.filter((capture) => displayedAccountIds.has(captureAccountKey(capture)))
    : reports.captures;
  const displayedTotals = activeAccounts.length
    ? activeAccounts.reduce(
        (totals, account) => {
          totals.captures += account.captures;
          totals.xp += account.xp;
          totals.casts += account.casts;
          totals.automationSeconds += account.automationSeconds;
          for (const [name, count] of Object.entries(account.fish)) {
            totals.fish[name] = (totals.fish[name] || 0) + count;
          }
          return totals;
        },
        { captures: 0, xp: 0, casts: 0, automationSeconds: 0, fish: {} as Record<string, number> },
      )
    : {
        captures: reports.totals.captures,
        xp: reports.totals.xp,
        casts: reports.totals.casts,
        automationSeconds: reports.totals.automationSeconds,
        fish: reports.totals.fish,
      };
  const fishPerMinute = displayedTotals.automationSeconds
    ? (displayedTotals.captures * 60) / displayedTotals.automationSeconds
    : 0;
  const xpPerHour = displayedTotals.automationSeconds
    ? (displayedTotals.xp * 3600) / displayedTotals.automationSeconds
    : 0;
  const fish = Object.entries(displayedTotals.fish).sort((a, b) => b[1] - a[1]);
  return (
    <div className="report-workspace mt-6 space-y-5">
      <div className="report-kpis">
        <Metric
          label={activeAccounts.length ? "Capturas agora" : "Capturas efetivas"}
          value={number(displayedTotals.captures)}
        />
        <Metric
          label={activeAccounts.length ? "XP nesta execução" : "XP acumulado"}
          value={number(displayedTotals.xp)}
        />
        <Metric
          label="Média geral"
          value={`${number(fishPerMinute, 2)}/min`}
        />
        <Metric
          label="Projeção"
          value={`${number(xpPerHour)} XP/h`}
        />
        <Metric
          label="Timeouts"
          value={number(displayedAccounts.reduce((total, account) => total + account.timeouts, 0))}
        />
        <Metric
          label="Disputas evitadas"
          value={number(displayedAccounts.reduce((total, account) => total + account.targetContentions, 0))}
        />
      </div>

      <p className="report-goal">
        Meta operacional: <b>2,00 peixes/min por conta</b> · {displayedAccounts.filter((account) => account.fishPerMinute < 2).length} conta(s) abaixo da meta nesta execução.
      </p>

      {selectedAccount && (
        <section className="card account-detail">
          <div className="account-detail-header">
            <div>
              <p className="text-xs font-bold uppercase tracking-[0.18em] text-cyan-300">Conta selecionada</p>
              <h3 className="mt-1 font-bold">{selectedAccount.account}</h3>
              <p className="text-xs text-slate-500">Nível {fishingLevelLabel(selectedAccount)} · sessão {selectedAccount.sessionId.slice(-6)}</p>
            </div>
            <span className={`badge ${selectedAccount.currentlyFishing ? "" : "inactive"}`}>
              {selectedAccount.currentlyFishing ? "pescando" : "parado"}
            </span>
          </div>
          <div className="account-stats">
            <ReportStat label="Capturas" value={number(selectedAccount.captures)} />
            <ReportStat label="Peixes/min" value={number(selectedAccount.fishPerMinute, 2)} />
            <ReportStat label="XP/h" value={number(selectedAccount.xpPerHour)} />
            <ReportStat label="Sucesso" value={`${number(selectedAccount.successRate, 1)}%`} />
            <ReportStat label="Timeouts" value={number(selectedAccount.timeouts)} />
            <ReportStat label="Sem aceite" value={number(selectedAccount.noAckTimeouts)} />
            <ReportStat label="Sem mordida" value={number(selectedAccount.biteTimeouts)} />
            <ReportStat label="Resultado pendente" value={number(selectedAccount.resultTimeouts)} />
            <ReportStat label="Disputas evitadas" value={number(selectedAccount.targetContentions)} />
            <ReportStat label="Rotas revisadas" value={number(selectedAccount.routesBlocked)} />
            <ReportStat label="Tempo ativo" value={duration(selectedAccount.automationSeconds)} />
          </div>
          <div className={`account-goal ${selectedAccount.fishPerMinute >= 2 ? "on-target" : ""}`}>
            <span>{selectedAccount.fishPerMinute >= 2 ? "Meta atingida" : "Falta para a meta"}</span>
            <strong>{selectedAccount.fishPerMinute >= 2 ? "+" : ""}{number(selectedAccount.fishPerMinute - 2, 2)}/min</strong>
            <small>Meta: 2,00 peixes/min</small>
          </div>
          <PerformanceLineChart captures={selectedHistory} account={selectedAccount.account} />
        </section>
      )}

      <section className="card">
        <div className="flex flex-wrap items-end justify-between gap-3">
          <div>
            <h3 className="font-bold">
              {activeAccounts.length ? "Sessões pescando agora" : "Histórico com capturas"}
            </h3>
            <p className="text-xs text-slate-500">
              Capturas confirmadas pelo servidor, atualizadas em tempo real
            </p>
          </div>
          <div className="flex flex-wrap items-center gap-3">
            <span className="text-xs text-slate-500">Atualizado {when(reports.updatedAt)}</span>
            <button className="danger small" disabled={resetting} onClick={onReset}>
              {resetting ? "Zerando…" : "Zerar relatórios"}
            </button>
          </div>
        </div>
        <div className="report-toolbar">
          <input
            className="input report-search"
            value={reportQuery}
            onChange={(event) => setReportQuery(event.target.value)}
            placeholder="Buscar conta"
            aria-label="Buscar conta nos relatórios"
          />
          <div className="report-filters" role="group" aria-label="Filtrar relatórios">
            <button type="button" className={reportFilter === "all" ? "active" : ""} onClick={() => setReportFilter("all")}>
              Todas ({displayedAccounts.length})
            </button>
            <button type="button" className={reportFilter === "below-goal" ? "active" : ""} onClick={() => setReportFilter("below-goal")}>
              Abaixo da meta ({displayedAccounts.filter((account) => account.fishPerMinute < 2).length})
            </button>
            <button type="button" className={reportFilter === "alerts" ? "active" : ""} onClick={() => setReportFilter("alerts")}>
              Com alerta ({displayedAccounts.filter((account) => account.timeouts > 0 || account.routesBlocked > 0 || account.targetContentions > 0).length})
            </button>
          </div>
        </div>
        <div className="mt-4 space-y-3">
          {visibleAccounts.length ? (
            visibleAccounts.map((account) => (
              <button
                key={reportAccountKey(account)}
                type="button"
                onClick={() => setSelectedAccountId(reportAccountKey(account))}
                className={`account-report account-report-button ${reportAccountKey(selectedAccount || account) === reportAccountKey(account) ? "selected" : ""}`}
                aria-pressed={reportAccountKey(selectedAccount || account) === reportAccountKey(account)}
              >
                <div className="account-report-title">
                  <div>
                    <div className="flex flex-wrap items-center gap-2">
                      <strong>{account.account}</strong>
                      <span className="login-mode">{account.loginMode || "sessão"}</span>
                    </div>
                    <p className="text-xs text-slate-500">
                      {account.currentStatus || "fora da sessão"} · sessão {account.sessionId.slice(-6)} · última captura {when(account.lastCaptureAt)}
                    </p>
                  </div>
                  <span className={`account-health ${account.timeouts || account.routesBlocked || account.targetContentions ? "warning" : "healthy"}`}>
                    {account.timeouts || account.routesBlocked || account.targetContentions ? "Atenção" : "Estável"}
                  </span>
                </div>
                <div className="account-list-summary">
                  <span><small>Ritmo</small><b>{number(account.fishPerMinute, 2)}/min</b></span>
                  <span><small>Capturas</small><b>{number(account.captures)}</b></span>
                  <span><small>Timeouts</small><b>{number(account.timeouts)}</b></span>
                  <span><small>Rotas</small><b>{number(account.routesBlocked)}</b></span>
                  <span><small>Disputas</small><b>{number(account.targetContentions)}</b></span>
                  <span className={account.fishPerMinute >= 2 ? "target-good" : "target-warn"}><small>Meta</small><b>{account.fishPerMinute >= 2 ? "OK" : "Abaixo"}</b></span>
                </div>
              </button>
            ))
          ) : (
            <Empty text="Nenhuma conta encontrada com este filtro." />
          )}
        </div>
      </section>

      <div className="grid gap-5 xl:grid-cols-[.8fr_1.2fr]">
        <section className="card">
          <h3 className="font-bold">Peixes capturados</h3>
          <p className="mb-4 text-xs text-slate-500">Espécies e quantidade</p>
          <div className="fish-list">
            {fish.length ? (
              fish.map(([name, count]) => (
                <div key={name} className="fish-chip">
                  <span>{name}</span>
                  <strong>{count}</strong>
                </div>
              ))
            ) : (
              <Empty text="Nenhuma captura registrada ainda." />
            )}
          </div>
        </section>
        <section className="card min-w-0">
          <h3 className="font-bold">Últimas capturas</h3>
          <p className="mb-4 text-xs text-slate-500">Até 100 confirmações recentes</p>
          {displayedCaptures.length ? (
            <div className="capture-table">
              <div className="capture-row muted">
                <b>Horário</b><b>Conta</b><b>Peixe</b><b>XP</b>
              </div>
              {displayedCaptures.map((capture, index) => (
                <div key={`${capture.sessionId}-${capture.at}-${index}`} className="capture-row">
                  <span className="text-slate-400">{when(capture.at)}</span>
                  <strong>{capture.account}</strong>
                  <span>{capture.fish}</span>
                  <span className="font-bold text-emerald-300">+{capture.xp}</span>
                </div>
              ))}
            </div>
          ) : (
            <Empty text="Aguardando uma captura confirmada pelo servidor." />
          )}
        </section>
      </div>
    </div>
  );
}
function ReportStat({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <p className="text-[10px] uppercase tracking-wider text-slate-500">{label}</p>
      <p className="mt-1 font-mono text-sm font-bold text-cyan-100">{value}</p>
    </div>
  );
}
function PerformanceLineChart({ captures, account }: { captures: Capture[]; account: string }) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const context = canvas.getContext("2d");
    if (!context) return;
    const width = canvas.clientWidth || 640;
    const height = canvas.clientHeight || 190;
    const ratio = window.devicePixelRatio || 1;
    canvas.width = Math.floor(width * ratio);
    canvas.height = Math.floor(height * ratio);
    context.scale(ratio, ratio);
    context.clearRect(0, 0, width, height);
    context.strokeStyle = "rgba(148, 163, 184, 0.18)";
    context.lineWidth = 1;
    for (let row = 1; row < 4; row++) {
      const y = (height / 4) * row;
      context.beginPath();
      context.moveTo(0, y);
      context.lineTo(width, y);
      context.stroke();
    }
    if (!captures.length) return;
    const ordered = [...captures].sort((a, b) => new Date(a.at).getTime() - new Date(b.at).getTime());
    const times = ordered.map((item) => new Date(item.at).getTime());
    const first = times[0];
    const last = times[times.length - 1];
    const span = Math.max(1, last - first);
    const padding = { left: 10, right: 10, top: 12, bottom: 20 };
    const graphWidth = width - padding.left - padding.right;
    const graphHeight = height - padding.top - padding.bottom;
    context.strokeStyle = "#22d3ee";
    context.lineWidth = 2.5;
    context.lineJoin = "round";
    context.lineCap = "round";
    context.beginPath();
    ordered.forEach((item, index) => {
      const x = padding.left + ((times[index] - first) / span) * graphWidth;
      const y = padding.top + graphHeight - ((index + 1) / Math.max(1, ordered.length)) * graphHeight;
      if (index === 0) context.moveTo(x, y);
      else context.lineTo(x, y);
    });
    context.stroke();
    const lastX = padding.left + graphWidth;
    const lastY = padding.top;
    context.fillStyle = "#a7f3d0";
    context.beginPath();
    context.arc(lastX, lastY, 3.5, 0, Math.PI * 2);
    context.fill();
  }, [captures]);
  return (
    <div className="performance-chart">
      <div className="performance-chart-title">
        <span>Progressão de capturas</span>
        <small>{captures.length ? `${captures.length} ponto(s) registrados` : `Aguardando capturas de ${account}`}</small>
      </div>
      <canvas ref={canvasRef} aria-label={`Gráfico de progressão de capturas de ${account}`} />
      <div className="performance-chart-axis"><span>Início da sessão</span><span>Agora</span></div>
    </div>
  );
}
function Status({ online }: { online: boolean }) {
  return (
    <div className={`status ${online ? "on" : "off"}`}>
      <span />
      {online ? "Motor online" : "Motor offline"}
    </div>
  );
}
function GEarthStatusPill({ status }: { status: GEarthStatus }) {
  const online = status.running && status.ready;
  return (
    <div className={`status ${online ? "on" : "off"}`} title={status.state}>
      <span />
      {online ? "Codex G-Earth integrado" : "Núcleo indisponível"}
    </div>
  );
}
function Metric({ label, value }: { label: string; value: string | number }) {
  return (
    <div className="card">
      <p className="text-sm text-slate-500">{label}</p>
      <p className="mt-2 text-3xl font-black">{value}</p>
    </div>
  );
}
function Panel({
  title,
  sub,
  children,
}: {
  title: string;
  sub: string;
  children: ReactNode;
}) {
  return (
    <section className="card">
      <h3 className="font-bold">{title}</h3>
      <p className="mb-4 text-xs text-slate-500">{sub}</p>
      <div className="space-y-2">{children}</div>
    </section>
  );
}
function Row({
  title,
  sub,
  children,
}: {
  title: string;
  sub: string;
  children: ReactNode;
}) {
  return (
    <div className="row">
      <div className="min-w-0 flex-1">
        <p className="truncate font-bold">{title}</p>
        <p className="truncate text-xs text-slate-500">{sub}</p>
      </div>
      {children}
    </div>
  );
}
function Empty({ text }: { text: string }) {
  return <p className="empty">{text}</p>;
}
function Modal({
  title,
  close,
  children,
}: {
  title: string;
  close: () => void;
  children: ReactNode;
}) {
  useEffect(() => {
    const previousOverflow = document.body.style.overflow;
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") close();
    };
    document.body.style.overflow = "hidden";
    document.addEventListener("keydown", handleKeyDown);
    return () => {
      document.body.style.overflow = previousOverflow;
      document.removeEventListener("keydown", handleKeyDown);
    };
  }, [close]);
  return (
    <div
      className="modal"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) close();
      }}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label={title}
        className="dialog"
      >
        <div className="mb-5 flex justify-between">
          <h3 className="text-2xl font-black">{title}</h3>
          <button
            autoFocus
            onClick={close}
            aria-label="Fechar"
            className="text-2xl text-slate-400"
          >
            ×
          </button>
        </div>
        {children}
      </div>
    </div>
  );
}
function Input({
  value,
  set,
  placeholder,
  type = "text",
}: {
  value: string;
  set: (v: string) => void;
  placeholder: string;
  type?: string;
}) {
  return (
    <input
      type={type}
      value={value}
      onChange={(e) => set(e.target.value)}
      placeholder={placeholder}
      aria-label={placeholder}
      className="input"
    />
  );
}

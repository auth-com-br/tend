#!/usr/bin/env python3
"""Generate the tend site in en, pt-BR, es, ja and zh-CN."""

from pathlib import Path

ROOT = Path(__file__).resolve().parent
INSTALL = "curl -fsSL https://tend.auth.com.br/install.sh | sh"

# Keys shared by every locale. Pane mock stays English on purpose: it is what
# the program draws, not marketing copy.
LANGS = {
    "en": {
        "html_lang": "en",
        "dir": ".",
        "href": {
            "en": "./",
            "pt": "pt/",
            "es": "es/",
            "ja": "ja/",
            "zh": "zh/",
        },
        "title": "tend — from the ticket and the error to the PR, with agents",
        "description": "tend takes your GitHub issues and your production errors, opens a worktree and hands the task to your coding agent. You follow every agent, and close what was fixed, on one screen.",
        "nav_session": "session",
        "nav_tools": "tools",
        "nav_keys": "keys",
        "nav_install": "install",
        "nav_source": "source",
        "os_tabs_label": "Operating system",
        "os_linux": "Linux",
        "os_macos": "macOS",
        "os_windows": "Windows",
        "detected_linux": "detected Linux",
        "detected_macos": "detected macOS",
        "detected_windows": "detected Windows",
        "download_prefix": "Download",
        "all_builds": "all builds",
        "windows_p1": "tend is Linux and macOS only. There is no Windows binary.",
        "windows_p2": "On Windows, run the Linux installer inside WSL, or clone and <code>make install</code> on a Unix machine. The <a href=\"https://github.com/auth-com-br/tend/blob/master/docs/WINDOWS.md\">WSL guide</a> walks through it.",
        "deck": "From the ticket and the production error to the pull request, with agents, without leaving the terminal. tend takes a GitHub issue or a GlitchTip error, opens a worktree and hands it to Claude Code, Codex or any agent you use. You follow them all — each pane marked working, blocked or idle — and close what was fixed, on the same screen.",
        "stage_label": "A tend session",
        "stage_cap": "A session. The server is still there when nobody is looking.",
        "facts_h": "What stays when you detach",
        "fact1_t": "The programs",
        "fact1_d": "A session is a background server. <kbd>ctrl</kbd><kbd>b</kbd> then <kbd>d</kbd> leaves the agents running. Opening tend again attaches to the same panes.",
        "fact2_t": "The state",
        "fact2_d": "A pane is working, blocked, or idle. The mark is what the screen is showing — a spinner, a permission prompt, a shell waiting — not a guess about the process name.",
        "fact3_t": "The layout",
        "fact3_d": "A space holds tabs. A tab holds panes. Split beside or below, zoom one pane, scroll its history, jump to the agent that stopped.",
        "k_swap": "swap panes",
        "tools_h": "Tools, over the spaces",
        "tools_note": "Buttons at the top of the sidebar, each with a key after the prefix.",
        "tool1_t": "Files <kbd>f</kbd>",
        "tool1_d": "A panel beside your panes: the project's tree, a search through it, and its git changes — diff, stage, commit, push. It follows the pane you are working in.",
        "tool2_t": "Agents <kbd>A</kbd>",
        "tool2_d": "The agent CLIs tend knows — Claude Code, Codex, Gemini CLI, Cursor, OpenCode and more — which this machine has, and the install command for the rest, run in a tab once you say yes.",
        "tool3_t": "Sessions <kbd>S</kbd>",
        "tool3_d": "Every Claude Code conversation on the machine, by its title. Type to search, enter to resume one in a new tab where it was held, mark old ones and delete them.",
        "tool4_t": "GitHub · GitLab <kbd>I</kbd>",
        "tool4_d": "The project's issues and pull requests, through <code>gh</code>. Comment, close, label, merge. <kbd>w</kbd> on an issue makes a worktree for it and starts your agent there, told to complete it. Projects on GitLab — gitlab.com or your company's — work the same, with their merge requests.",
        "tool5_t": "Browser and context <kbd>B</kbd> <kbd>C</kbd>",
        "tool5_d": "A Chromium, Chrome or Edge window with tend's extension: pick elements on a page, note what you want of each, and send them to the agent, looked over first in the context panel.",
        "tool6_t": "Updates",
        "tool6_d": "tend notices a new release and says so. <kbd>u</kbd> in its notes installs it and moves the running session onto it, without stopping the programs in its panes.",
        "tool7_t": "Errors <kbd>E</kbd> <kbd>T</kbd>",
        "tool7_d": "What your systems' GlitchTip servers caught, as many servers as you report to. Open an error to read its stack; <kbd>f</kbd> hands it to your agent, <kbd>w</kbd> fixes it in a worktree, <kbd>ctrl</kbd><kbd>x</kbd> resolves it. Each project can open on its own server. Tickets from Linear, Jira, ClickUp and a self-hosted Issue Gateway are in a panel of their own, <kbd>T</kbd>, with the same way to hand one to your agent.",
        "tool8_t": "Companies <kbd>O</kbd>",
        "tool8_d": "Working for several companies? Keep each one's spaces together and choose one: the sidebar shows that company's projects and agents, and an agent waiting in another is still flagged.",
        "issues_cap": "The issues panel, prefix+I. <kbd>w</kbd> on #42 makes a worktree on <code>issue-42-checkout-button-is-grey</code> and starts your agent in it.",
        "sessions_cap": "The sessions list, prefix+S. Enter resumes one in a new tab, where it was held; <kbd>ctrl</kbd><kbd>o</kbd> marks what is a month old, <kbd>ctrl</kbd><kbd>d</kbd> deletes it. One open in a pane is never deleted.",
        "keys_h": "Prefix is ctrl+b",
        "keys_note": "<kbd>ctrl</kbd><kbd>b</kbd> <kbd>?</kbd> lists every key inside the session.",
        "k_split": "split beside, below",
        "k_focus": "move focus",
        "k_resize": "resize (then h j k l, esc)",
        "k_zoom": "zoom a pane to the window",
        "k_scroll": "scroll back through its history",
        "k_close": "close a pane",
        "k_tabs": "tabs: new, next, previous, by number",
        "k_spaces": "spaces: new, previous, next",
        "k_agents": "show every agent, grouped",
        "k_jump": "pick one and jump to it",
        "k_detach": "detach, leaving everything running",
        "install_h": "Install",
        "copy": "copy",
        "copied": "copied",
        "install_p1": "Puts a binary in <code>~/.local/bin</code> (or <code>$GOBIN</code> / <code>$TEND_INSTALL_DIR</code>). Prefers a release build for this machine; otherwise clones and builds with Go.",
        "install_note": "From a checkout: <code>make install</code>.",
        "install_p2": "With no arguments, tend opens a session and starts a server if there is not one. <code>tend new -- claude</code> opens a pane. <code>tend ls</code> lists what is running. <code>-s NAME</code> runs another session beside the first.",
        "install_p3": "Written in Go. One binary. No cgo.",
        "footer": "Apache-2.0. An independent Go implementation; see NOTICE in the repository.",
        "made_by": "A product of",
    },
    "pt": {
        "html_lang": "pt-BR",
        "dir": "pt",
        "href": {
            "en": "../",
            "pt": "./",
            "es": "../es/",
            "ja": "../ja/",
            "zh": "../zh/",
        },
        "title": "tend — do ticket e do erro até o PR, com agentes",
        "description": "O tend pega as issues do GitHub e os erros de produção, abre um worktree e entrega a tarefa ao seu agente de código. Você acompanha todos os agentes, e fecha o que foi resolvido, na mesma tela.",
        "nav_session": "sessão",
        "nav_tools": "ferramentas",
        "nav_keys": "teclas",
        "nav_install": "instalar",
        "nav_source": "código",
        "os_tabs_label": "Sistema operacional",
        "os_linux": "Linux",
        "os_macos": "macOS",
        "os_windows": "Windows",
        "detected_linux": "detectado Linux",
        "detected_macos": "detectado macOS",
        "detected_windows": "detectado Windows",
        "download_prefix": "Baixar",
        "all_builds": "todos os builds",
        "windows_p1": "O tend é só Linux e macOS. Não há binário para Windows.",
        "windows_p2": "No Windows, rode o instalador Linux no WSL, ou clone e <code>make install</code> numa máquina Unix. O <a href=\"https://github.com/auth-com-br/tend/blob/master/docs/WINDOWS.md\">guia do WSL</a> mostra o caminho.",
        "deck": "Do ticket e do erro de produção até o pull request, com agentes, sem sair do terminal. O tend pega uma issue do GitHub ou um erro do GlitchTip, abre um worktree e entrega ao Claude Code, ao Codex ou ao agente que você usa. Você acompanha todos — cada painel marcado como working, blocked ou idle — e fecha o que foi resolvido, na mesma tela.",
        "stage_label": "Uma sessão do tend",
        "stage_cap": "Uma sessão. O servidor continua quando ninguém está olhando.",
        "facts_h": "O que fica quando você desconecta",
        "fact1_t": "Os programas",
        "fact1_d": "Uma sessão é um servidor em segundo plano. <kbd>ctrl</kbd><kbd>b</kbd> e depois <kbd>d</kbd> deixa os agentes rodando. Abrir o tend de novo volta aos mesmos painéis.",
        "fact2_t": "O estado",
        "fact2_d": "Um painel está working, blocked ou idle. A marca é o que a tela mostra — um spinner, um pedido de permissão, um shell à espera — não um chute pelo nome do processo.",
        "fact3_t": "O layout",
        "fact3_d": "Um space guarda abas. Uma aba guarda painéis. Divida ao lado ou abaixo, amplie um painel, role o histórico, salte para o agente que parou.",
        "k_swap": "trocar painéis de lugar",
        "tools_h": "Ferramentas, acima dos spaces",
        "tools_note": "Botões no topo da barra lateral, cada um com uma tecla depois do prefixo.",
        "tool1_t": "Arquivos <kbd>f</kbd>",
        "tool1_d": "Um painel ao lado dos seus: a árvore do projeto, uma busca nela e as mudanças do git — diff, stage, commit, push. Ele segue o painel em que você está trabalhando.",
        "tool2_t": "Agentes <kbd>A</kbd>",
        "tool2_d": "Os CLIs de agentes que o tend conhece — Claude Code, Codex, Gemini CLI, Cursor, OpenCode e outros —, quais esta máquina tem e o comando para instalar os outros, rodado numa aba quando você confirma.",
        "tool3_t": "Sessões <kbd>S</kbd>",
        "tool3_d": "Todas as conversas do Claude Code na máquina, pelo título. Digite para buscar, enter retoma uma numa aba nova onde ela aconteceu, marque as antigas e apague.",
        "tool4_t": "GitHub · GitLab <kbd>I</kbd>",
        "tool4_d": "As issues e os pull requests do projeto, pelo <code>gh</code>. Comente, feche, ponha rótulos, faça merge. <kbd>w</kbd> numa issue cria um worktree para ela e abre seu agente lá, com a issue como tarefa. Projetos no GitLab — o gitlab.com ou o da sua empresa — funcionam igual, com os merge requests.",
        "tool5_t": "Navegador e contexto <kbd>B</kbd> <kbd>C</kbd>",
        "tool5_d": "Uma janela do Chromium, Chrome ou Edge com a extensão do tend: escolha elementos numa página, anote o que quer de cada um e mande para o agente, revisando antes no painel de contexto.",
        "tool6_t": "Atualizações",
        "tool6_d": "O tend percebe um release novo e avisa. <kbd>u</kbd> nas notas instala e passa a sessão para a versão nova, sem parar os programas dos painéis.",
        "tool7_t": "Erros <kbd>E</kbd> <kbd>T</kbd>",
        "tool7_d": "O que os servidores GlitchTip dos seus sistemas pegaram, quantos servidores você tiver. Abra um erro para ler a pilha; <kbd>f</kbd> entrega ao seu agente, <kbd>w</kbd> conserta num worktree, <kbd>ctrl</kbd><kbd>x</kbd> resolve. Cada projeto pode abrir no seu próprio servidor. Os tickets do Linear, Jira, ClickUp e de um Issue Gateway próprio têm um painel próprio, <kbd>T</kbd>, com o mesmo jeito de entregar ao agente.",
        "tool8_t": "Empresas <kbd>O</kbd>",
        "tool8_d": "Atende várias empresas? Guarde os spaces de cada uma juntos e escolha uma: a barra lateral mostra os projetos e agentes daquela empresa, e um agente esperando em outra continua sendo avisado.",
        "issues_cap": "O painel de issues, prefix+I. <kbd>w</kbd> na #42 cria um worktree em <code>issue-42-checkout-button-is-grey</code> e abre seu agente nele.",
        "sessions_cap": "A lista de sessões, prefix+S. Enter retoma uma numa aba nova, onde ela aconteceu; <kbd>ctrl</kbd><kbd>o</kbd> marca as de um mês atrás, <kbd>ctrl</kbd><kbd>d</kbd> apaga. Uma sessão aberta num painel nunca é apagada.",
        "keys_h": "O prefixo é ctrl+b",
        "keys_note": "<kbd>ctrl</kbd><kbd>b</kbd> <kbd>?</kbd> lista todas as teclas dentro da sessão.",
        "k_split": "dividir ao lado, abaixo",
        "k_focus": "mover o foco",
        "k_resize": "redimensionar (depois h j k l, esc)",
        "k_zoom": "ampliar um painel na janela",
        "k_scroll": "voltar no histórico",
        "k_close": "fechar um painel",
        "k_tabs": "abas: nova, seguinte, anterior, por número",
        "k_spaces": "spaces: novo, anterior, seguinte",
        "k_agents": "mostrar todos os agentes, agrupados",
        "k_jump": "escolher um e ir até ele",
        "k_detach": "desconectar, deixando tudo rodando",
        "install_h": "Instalar",
        "copy": "copiar",
        "copied": "copiado",
        "install_p1": "Coloca o binário em <code>~/.local/bin</code> (ou <code>$GOBIN</code> / <code>$TEND_INSTALL_DIR</code>). Prefere um release para esta máquina; senão clona e compila com Go.",
        "install_note": "A partir de um checkout: <code>make install</code>.",
        "install_p2": "Sem argumentos, o tend abre uma sessão e sobe um servidor se ainda não houver. <code>tend new -- claude</code> abre um painel. <code>tend ls</code> lista o que está rodando. <code>-s NAME</code> roda outra sessão ao lado da primeira.",
        "install_p3": "Escrito em Go. Um binário. Sem cgo.",
        "footer": "Apache-2.0. Implementação independente em Go; veja NOTICE no repositório.",
        "made_by": "Um produto da",
    },
    "es": {
        "html_lang": "es",
        "dir": "es",
        "href": {
            "en": "../",
            "pt": "../pt/",
            "es": "./",
            "ja": "../ja/",
            "zh": "../zh/",
        },
        "title": "tend — del ticket y el error al PR, con agentes",
        "description": "tend toma las issues de GitHub y los errores de producción, abre un worktree y entrega la tarea a tu agente de código. Sigues a todos los agentes, y cierras lo resuelto, en la misma pantalla.",
        "nav_session": "sesión",
        "nav_tools": "herramientas",
        "nav_keys": "teclas",
        "nav_install": "instalar",
        "nav_source": "código",
        "os_tabs_label": "Sistema operativo",
        "os_linux": "Linux",
        "os_macos": "macOS",
        "os_windows": "Windows",
        "detected_linux": "detectado Linux",
        "detected_macos": "detectado macOS",
        "detected_windows": "detectado Windows",
        "download_prefix": "Descargar",
        "all_builds": "todas las builds",
        "windows_p1": "tend es solo Linux y macOS. No hay binario para Windows.",
        "windows_p2": "En Windows, ejecuta el instalador Linux en WSL, o clona y <code>make install</code> en una máquina Unix. La <a href=\"https://github.com/auth-com-br/tend/blob/master/docs/WINDOWS.md\">guía de WSL</a> lo explica paso a paso.",
        "deck": "Del ticket y el error de producción al pull request, con agentes, sin salir de la terminal. tend toma una issue de GitHub o un error de GlitchTip, abre un worktree y se lo entrega a Claude Code, Codex o el agente que uses. Los sigues a todos — cada panel marcado como working, blocked o idle — y cierras lo resuelto, en la misma pantalla.",
        "stage_label": "Una sesión de tend",
        "stage_cap": "Una sesión. El servidor sigue ahí cuando nadie mira.",
        "facts_h": "Qué queda al desconectarte",
        "fact1_t": "Los programas",
        "fact1_d": "Una sesión es un servidor en segundo plano. <kbd>ctrl</kbd><kbd>b</kbd> y luego <kbd>d</kbd> deja los agentes en marcha. Abrir tend otra vez vuelve a los mismos paneles.",
        "fact2_t": "El estado",
        "fact2_d": "Un panel está working, blocked o idle. La marca es lo que muestra la pantalla — un spinner, un permiso, un shell a la espera — no una conjetura por el nombre del proceso.",
        "fact3_t": "El diseño",
        "fact3_d": "Un space guarda pestañas. Una pestaña guarda paneles. Divide al lado o abajo, amplía un panel, recorre el historial, salta al agente que se detuvo.",
        "k_swap": "intercambiar paneles",
        "tools_h": "Herramientas, sobre los spaces",
        "tools_note": "Botones arriba de la barra lateral, cada uno con una tecla tras el prefijo.",
        "tool1_t": "Archivos <kbd>f</kbd>",
        "tool1_d": "Un panel junto a los tuyos: el árbol del proyecto, una búsqueda en él y los cambios de git — diff, stage, commit, push. Sigue al panel en el que trabajas.",
        "tool2_t": "Agentes <kbd>A</kbd>",
        "tool2_d": "Los CLI de agentes que tend conoce — Claude Code, Codex, Gemini CLI, Cursor, OpenCode y más —, cuáles tiene esta máquina y el comando para instalar el resto, ejecutado en una pestaña cuando confirmas.",
        "tool3_t": "Sesiones <kbd>S</kbd>",
        "tool3_d": "Todas las conversaciones de Claude Code en la máquina, por su título. Escribe para buscar, enter reanuda una en una pestaña nueva donde ocurrió, marca las viejas y bórralas.",
        "tool4_t": "GitHub · GitLab <kbd>I</kbd>",
        "tool4_d": "Los issues y pull requests del proyecto, a través de <code>gh</code>. Comenta, cierra, etiqueta, fusiona. <kbd>w</kbd> en un issue crea un worktree para él y abre tu agente allí, con el issue como tarea. Los proyectos en GitLab — gitlab.com o el de tu empresa — funcionan igual, con sus merge requests.",
        "tool5_t": "Navegador y contexto <kbd>B</kbd> <kbd>C</kbd>",
        "tool5_d": "Una ventana de Chromium, Chrome o Edge con la extensión de tend: elige elementos en una página, anota lo que quieres de cada uno y envíalos al agente, revisados antes en el panel de contexto.",
        "tool6_t": "Actualizaciones",
        "tool6_d": "tend detecta una nueva versión y lo dice. <kbd>u</kbd> en sus notas la instala y pasa la sesión a ella, sin detener los programas de los paneles.",
        "tool7_t": "Errores <kbd>E</kbd> <kbd>T</kbd>",
        "tool7_d": "Lo que capturaron los servidores GlitchTip de tus sistemas, tantos como uses. Abre un error para leer su traza; <kbd>f</kbd> se lo entrega a tu agente, <kbd>w</kbd> lo arregla en un worktree, <kbd>ctrl</kbd><kbd>x</kbd> lo resuelve. Cada proyecto puede abrir en su propio servidor. Los tickets de Linear, Jira, ClickUp y de un Issue Gateway propio tienen su propio panel, <kbd>T</kbd>, con la misma forma de entregarlos al agente.",
        "tool8_t": "Empresas <kbd>O</kbd>",
        "tool8_d": "¿Trabajas para varias empresas? Agrupa los spaces de cada una y elige una: la barra lateral muestra sus proyectos y agentes, y un agente esperando en otra sigue avisándose.",
        "issues_cap": "El panel de issues, prefix+I. <kbd>w</kbd> en #42 crea un worktree en <code>issue-42-checkout-button-is-grey</code> y abre tu agente en él.",
        "sessions_cap": "La lista de sesiones, prefix+S. Enter reanuda una en una pestaña nueva, donde ocurrió; <kbd>ctrl</kbd><kbd>o</kbd> marca las de hace un mes, <kbd>ctrl</kbd><kbd>d</kbd> las borra. Una sesión abierta en un panel nunca se borra.",
        "keys_h": "El prefijo es ctrl+b",
        "keys_note": "<kbd>ctrl</kbd><kbd>b</kbd> <kbd>?</kbd> lista todas las teclas dentro de la sesión.",
        "k_split": "dividir al lado, abajo",
        "k_focus": "mover el foco",
        "k_resize": "redimensionar (luego h j k l, esc)",
        "k_zoom": "ampliar un panel a la ventana",
        "k_scroll": "volver en el historial",
        "k_close": "cerrar un panel",
        "k_tabs": "pestañas: nueva, siguiente, anterior, por número",
        "k_spaces": "spaces: nuevo, anterior, siguiente",
        "k_agents": "mostrar todos los agentes, agrupados",
        "k_jump": "elegir uno e ir a él",
        "k_detach": "desconectar, dejando todo en marcha",
        "install_h": "Instalar",
        "copy": "copiar",
        "copied": "copiado",
        "install_p1": "Deja el binario en <code>~/.local/bin</code> (o <code>$GOBIN</code> / <code>$TEND_INSTALL_DIR</code>). Prefiere un release para esta máquina; si no, clona y compila con Go.",
        "install_note": "Desde un checkout: <code>make install</code>.",
        "install_p2": "Sin argumentos, tend abre una sesión y arranca un servidor si no hay uno. <code>tend new -- claude</code> abre un panel. <code>tend ls</code> lista lo que corre. <code>-s NAME</code> abre otra sesión junto a la primera.",
        "install_p3": "Escrito en Go. Un binario. Sin cgo.",
        "footer": "Apache-2.0. Implementación independiente en Go; ver NOTICE en el repositorio.",
        "made_by": "Un producto de",
    },
    "ja": {
        "html_lang": "ja",
        "dir": "ja",
        "href": {
            "en": "../",
            "pt": "../pt/",
            "es": "../es/",
            "ja": "./",
            "zh": "../zh/",
        },
        "title": "tend — チケットとエラーから PR まで、エージェントと",
        "description": "tend は GitHub の issue と本番環境のエラーを取り込み、worktree を作ってコーディングエージェントに渡します。すべてのエージェントの様子と、解決した項目のクローズを、ひとつの画面で。",
        "nav_session": "セッション",
        "nav_tools": "ツール",
        "nav_keys": "キー",
        "nav_install": "インストール",
        "nav_source": "ソース",
        "os_tabs_label": "OS",
        "os_linux": "Linux",
        "os_macos": "macOS",
        "os_windows": "Windows",
        "detected_linux": "Linux を検出",
        "detected_macos": "macOS を検出",
        "detected_windows": "Windows を検出",
        "download_prefix": "ダウンロード",
        "all_builds": "すべてのビルド",
        "windows_p1": "tend は Linux と macOS のみです。Windows 用のバイナリはありません。",
        "windows_p2": "Windows では WSL 上で Linux 用インストーラを使うか、Unix でクローンして <code>make install</code> してください。 手順は <a href=\"https://github.com/auth-com-br/tend/blob/master/docs/WINDOWS.md\">WSL ガイド</a> を参照してください。",
        "deck": "チケットと本番エラーからプルリクエストまで、エージェントと一緒に、ターミナルから離れずに。tend は GitHub の issue や GlitchTip のエラーを受け取り、worktree を作って Claude Code や Codex など使っているエージェントに渡します。各ペインは working・blocked・idle と示され、解決したものは同じ画面で閉じられます。",
        "stage_label": "tend のセッション",
        "stage_cap": "セッション。誰も見ていなくてもサーバーは動き続けます。",
        "facts_h": "デタッチしても残るもの",
        "fact1_t": "プログラム",
        "fact1_d": "セッションはバックグラウンドのサーバーです。<kbd>ctrl</kbd><kbd>b</kbd> のあと <kbd>d</kbd> でエージェントは動き続けます。tend を開き直すと同じペインに戻ります。",
        "fact2_t": "状態",
        "fact2_d": "ペインは working・blocked・idle のいずれかです。印は画面が示しているもの — スピナー、許可プロンプト、待機中のシェル — であり、プロセス名からの推測ではありません。",
        "fact3_t": "レイアウト",
        "fact3_d": "space はタブを持ち、タブはペインを持ちます。横や下に分割し、ズームし、履歴をスクロールし、止まったエージェントへジャンプします。",
        "k_swap": "ペインを入れ替える",
        "tools_h": "スペースの上にあるツール",
        "tools_note": "サイドバー上部のボタン。それぞれプレフィックスの後のキーでも開けます。",
        "tool1_t": "ファイル <kbd>f</kbd>",
        "tool1_d": "ペインの横のパネル：プロジェクトのツリー、その中の検索、git の変更 — diff、stage、commit、push。作業中のペインに追従します。",
        "tool2_t": "エージェント <kbd>A</kbd>",
        "tool2_d": "tend が知っているエージェント CLI — Claude Code、Codex、Gemini CLI、Cursor、OpenCode など — のうち、このマシンにあるものと、残りのインストールコマンド。確認するとタブで実行します。",
        "tool3_t": "セッション <kbd>S</kbd>",
        "tool3_d": "マシン上の Claude Code の会話をタイトルで一覧。入力して検索、enter で元のディレクトリの新しいタブで再開、古いものはマークして削除。",
        "tool4_t": "GitHub · GitLab <kbd>I</kbd>",
        "tool4_d": "プロジェクトの issue と pull request を <code>gh</code> 経由で。コメント、クローズ、ラベル、マージ。issue で <kbd>w</kbd> を押すと worktree を作り、そこでエージェントに issue を完了させます。 GitLab（gitlab.com や社内の GitLab）のプロジェクトも、マージリクエストを含めて同じように使えます。",
        "tool5_t": "ブラウザとコンテキスト <kbd>B</kbd> <kbd>C</kbd>",
        "tool5_d": "tend の拡張機能入りの Chromium、Chrome、Edge：ページの要素を選び、それぞれにメモを付け、コンテキストパネルで確認してからエージェントへ送ります。",
        "tool6_t": "アップデート",
        "tool6_d": "新しいリリースを検知して知らせます。リリースノートで <kbd>u</kbd> を押すとインストールし、ペインのプログラムを止めずにセッションを移行します。",
        "tool7_t": "エラー <kbd>E</kbd> <kbd>T</kbd>",
        "tool7_d": "システムの GlitchTip サーバーが捕捉したエラー（複数サーバー対応）。エラーを開いてスタックを読み、<kbd>f</kbd> でエージェントに渡し、<kbd>w</kbd> で worktree で修正、<kbd>ctrl</kbd><kbd>x</kbd> で解決。プロジェクトごとに開くサーバーを決められます。 Linear・Jira・ClickUp・セルフホストの Issue Gateway のチケットは専用パネル <kbd>T</kbd> で、同じようにエージェントに渡せます。",
        "tool8_t": "会社 <kbd>O</kbd>",
        "tool8_d": "複数の会社の仕事をしていますか。会社ごとに space をまとめて選ぶと、サイドバーにはその会社のプロジェクトとエージェントだけが表示されます。ほかの会社で待っているエージェントも知らせます。",
        "issues_cap": "issue パネル（prefix+I）。#42 で <kbd>w</kbd> を押すと <code>issue-42-checkout-button-is-grey</code> に worktree を作り、そこでエージェントを起動します。",
        "sessions_cap": "セッション一覧（prefix+S）。enter で元の場所の新しいタブで再開。<kbd>ctrl</kbd><kbd>o</kbd> で1か月前のものをマークし、<kbd>ctrl</kbd><kbd>d</kbd> で削除。ペインで開いているものは削除されません。",
        "keys_h": "プレフィックスは ctrl+b",
        "keys_note": "<kbd>ctrl</kbd><kbd>b</kbd> <kbd>?</kbd> でセッション内の全キーを一覧します。",
        "k_split": "横・下に分割",
        "k_focus": "フォーカス移動",
        "k_resize": "サイズ変更（続けて h j k l、esc）",
        "k_zoom": "ペインをウィンドウいっぱいに",
        "k_scroll": "履歴をスクロール",
        "k_close": "ペインを閉じる",
        "k_tabs": "タブ: 新規・次・前・番号",
        "k_spaces": "スペース：新規、前、次",
        "k_agents": "エージェント一覧（グループ表示）",
        "k_jump": "選んでジャンプ",
        "k_detach": "デタッチ（実行はそのまま）",
        "install_h": "インストール",
        "copy": "コピー",
        "copied": "コピー済み",
        "install_p1": "バイナリを <code>~/.local/bin</code>（または <code>$GOBIN</code> / <code>$TEND_INSTALL_DIR</code>）に置きます。このマシン向けのリリースがあればそれを使い、なければ Go でクローンしてビルドします。",
        "install_note": "チェックアウトから: <code>make install</code>。",
        "install_p2": "引数なしで tend を実行するとセッションを開き、サーバーがなければ起動します。<code>tend new -- claude</code> でペインを開き、<code>tend ls</code> で一覧、<code>-s NAME</code> で別セッションを並べます。",
        "install_p3": "Go 製。単一バイナリ。cgo なし。",
        "footer": "Apache-2.0。独立した Go 実装。詳細はリポジトリの NOTICE を参照。",
        "made_by": "開発元:",
    },
    "zh": {
        "html_lang": "zh-CN",
        "dir": "zh",
        "href": {
            "en": "../",
            "pt": "../pt/",
            "es": "../es/",
            "ja": "../ja/",
            "zh": "./",
        },
        "title": "tend — 从工单和错误到 PR，交给智能体",
        "description": "tend 接收 GitHub issue 和生产环境错误，创建 worktree，并把任务交给你的编程智能体。你在同一个屏幕上跟进所有智能体，并关闭已解决的问题。",
        "nav_session": "会话",
        "nav_tools": "工具",
        "nav_keys": "快捷键",
        "nav_install": "安装",
        "nav_source": "源码",
        "os_tabs_label": "操作系统",
        "os_linux": "Linux",
        "os_macos": "macOS",
        "os_windows": "Windows",
        "detected_linux": "已检测 Linux",
        "detected_macos": "已检测 macOS",
        "detected_windows": "已检测 Windows",
        "download_prefix": "下载",
        "all_builds": "全部构建",
        "windows_p1": "tend 仅支持 Linux 和 macOS。没有 Windows 二进制文件。",
        "windows_p2": "在 Windows 上，请在 WSL 中运行 Linux 安装命令，或在 Unix 机器上克隆后执行 <code>make install</code>。 步骤见 <a href=\"https://github.com/auth-com-br/tend/blob/master/docs/WINDOWS.md\">WSL 指南</a>。",
        "deck": "从工单和生产错误到 pull request，交给智能体，不离开终端。tend 接收一个 GitHub issue 或 GlitchTip 错误，创建 worktree，交给 Claude Code、Codex 或你使用的任何智能体。每个窗格标为 working、blocked 或 idle，已解决的在同一屏幕上关闭。",
        "stage_label": "一个 tend 会话",
        "stage_cap": "一次会话。没人看着的时候，服务器也还在。",
        "facts_h": "断开后仍会留下的",
        "fact1_t": "程序",
        "fact1_d": "会话是后台服务器。<kbd>ctrl</kbd><kbd>b</kbd> 再按 <kbd>d</kbd> 后智能体继续运行。再次打开 tend 会回到同样的窗格。",
        "fact2_t": "状态",
        "fact2_d": "窗格是 working、blocked 或 idle。标记来自屏幕上的内容 — 转圈、权限提示、等待中的 shell — 不是根据进程名猜测。",
        "fact3_t": "布局",
        "fact3_d": "space 容纳标签。标签容纳窗格。左右或上下分割，放大一个窗格，滚动历史，跳到停下来的智能体。",
        "k_swap": "交换窗格",
        "tools_h": "空间上方的工具",
        "tools_note": "侧边栏顶部的按钮，每个都可在前缀后用一个键打开。",
        "tool1_t": "文件 <kbd>f</kbd>",
        "tool1_d": "窗格旁的面板：项目的文件树、在其中搜索，以及 git 改动 — diff、暂存、提交、推送。它跟随你正在工作的窗格。",
        "tool2_t": "代理 <kbd>A</kbd>",
        "tool2_d": "tend 认识的代理 CLI — Claude Code、Codex、Gemini CLI、Cursor、OpenCode 等 — 这台机器装了哪些，以及其余的安装命令，确认后在标签页中运行。",
        "tool3_t": "会话 <kbd>S</kbd>",
        "tool3_d": "本机所有 Claude Code 对话，按标题列出。输入即可搜索，回车在原目录的新标签页中恢复，标记旧会话并删除。",
        "tool4_t": "GitHub · GitLab <kbd>I</kbd>",
        "tool4_d": "通过 <code>gh</code> 查看项目的 issue 和 pull request。评论、关闭、打标签、合并。在 issue 上按 <kbd>w</kbd> 会为它创建 worktree，并在其中启动你的代理去完成它。 GitLab（gitlab.com 或公司自建的）上的项目同样可用，包括合并请求。",
        "tool5_t": "浏览器与上下文 <kbd>B</kbd> <kbd>C</kbd>",
        "tool5_d": "带 tend 扩展的 Chromium、Chrome 或 Edge 窗口：在页面上选取元素，为每个写下要求，先在上下文面板中查看，再发送给代理。",
        "tool6_t": "更新",
        "tool6_d": "tend 会发现新版本并提示。在更新说明中按 <kbd>u</kbd> 即可安装，并在不停止窗格中程序的情况下把会话迁移过去。",
        "tool7_t": "错误 <kbd>E</kbd> <kbd>T</kbd>",
        "tool7_d": "你的系统的 GlitchTip 服务器捕获的错误，支持多个服务器。打开错误查看堆栈；<kbd>f</kbd> 交给智能体，<kbd>w</kbd> 在 worktree 中修复，<kbd>ctrl</kbd><kbd>x</kbd> 标记解决。每个项目可以打开自己的服务器。 Linear、Jira、ClickUp 和自托管的 Issue Gateway 的工单有自己的面板 <kbd>T</kbd>，同样可以交给智能体。",
        "tool8_t": "公司 <kbd>O</kbd>",
        "tool8_d": "同时为多家公司工作？把每家公司的 space 放在一起并选择一家：侧边栏只显示该公司的项目和智能体，其他公司里等待的智能体仍会提示。",
        "issues_cap": "issue 面板（prefix+I）。在 #42 上按 <kbd>w</kbd>，会在 <code>issue-42-checkout-button-is-grey</code> 上创建 worktree 并在其中启动你的代理。",
        "sessions_cap": "会话列表（prefix+S）。回车在原位置的新标签页中恢复；<kbd>ctrl</kbd><kbd>o</kbd> 标记一个月前的会话，<kbd>ctrl</kbd><kbd>d</kbd> 删除。在窗格中打开的会话永远不会被删除。",
        "keys_h": "前缀是 ctrl+b",
        "keys_note": "<kbd>ctrl</kbd><kbd>b</kbd> <kbd>?</kbd> 列出会话内的全部快捷键。",
        "k_split": "左右、上下分割",
        "k_focus": "移动焦点",
        "k_resize": "调整大小（然后 h j k l，esc）",
        "k_zoom": "将窗格放大到窗口",
        "k_scroll": "回看历史",
        "k_close": "关闭窗格",
        "k_tabs": "标签：新建、下一个、上一个、按数字",
        "k_spaces": "空间：新建、上一个、下一个",
        "k_agents": "显示全部智能体（分组）",
        "k_jump": "选一个并跳转",
        "k_detach": "断开，保持一切运行",
        "install_h": "安装",
        "copy": "复制",
        "copied": "已复制",
        "install_p1": "将二进制放到 <code>~/.local/bin</code>（或 <code>$GOBIN</code> / <code>$TEND_INSTALL_DIR</code>）。优先使用适合此机器的发布包；否则用 Go 克隆并编译。",
        "install_note": "从源码目录：<code>make install</code>。",
        "install_p2": "不带参数时，tend 打开会话，若没有服务器则启动一个。<code>tend new -- claude</code> 打开窗格。<code>tend ls</code> 列出正在运行的。<code>-s NAME</code> 在旁边再开一个会话。",
        "install_p3": "用 Go 写成。单一二进制。无 cgo。",
        "footer": "Apache-2.0。独立的 Go 实现；详见仓库中的 NOTICE。",
        "made_by": "出品方：",
    },
}

# Staged session: the chrome is what tend draws (toolbar, two-line sidebar
# entries, tabs, files panel, status). The pane text is invented so the
# public site does not leak a live session. English on purpose — it is what
# the program prints, not marketing copy.
SESSIONS_MOCK = """\
      <div class="term">
        <div class="term-chrome" aria-hidden="true">
          <span class="term-dots"><i></i><i></i><i></i></span>
          <span class="term-title">tend — sessions</span>
        </div>
        <pre class="issues-mock"><span class="accent pick">AGENT SESSIONS</span>                               <span class="dim">2 marked · 5 of 38</span>
<span class="dim">search</span> shop<span class="caret"> </span>

  <span class="accent">●</span> Checkout redesign            <span class="dim">acme/shop       now   4.2M</span>
    Footer links overlap         <span class="dim">acme/shop        3h   812K</span>
<span class="sel"><span class="accent">✓</span>   Orders CSV export            acme/shop       34d   1.9M </span>
<span class="accent">✓</span>   Old payment experiment       <span class="dim">acme/shop       41d    96K</span>
  <span class="dim">✗ Shop setup as root           root/shop       52d   220K</span>

<span class="dim">type to search · enter resume · tab mark · ^o 30+ days · ^d delete · esc</span>
<span class="dim">● open in a pane · ✗ cannot be opened here</span></pre>
      </div>
"""


ISSUES_MOCK = """\
      <div class="term">
        <div class="term-chrome" aria-hidden="true">
          <span class="term-dots"><i></i><i></i><i></i></span>
          <span class="term-title">tend — issues</span>
        </div>
        <pre class="issues-mock"><span class="accent pick">GITHUB ISSUES · acme/shop</span>                              <span class="dim">4 of 4</span>
<span class="chip on"> open </span> <span class="dim"> assigned to me   created by me   closed </span>   <span class="accent pick">Issues</span> <span class="dim">Pull requests</span>
<span class="dim">search</span> label:bug<span class="caret"> </span>

<span class="dim">#     title                              labels       author   age</span>
<span class="sel">42    Checkout button is grey on mobile  bug          ana      3h  </span>
<span class="accent">41</span>    Footer links overlap the badge     <span class="dim">bug, ui      bo       1d</span>
<span class="accent">38</span>    Dark mode for the settings page    <span class="dim">enhancement  ana      2d</span>
<span class="accent">35</span>    Export orders as CSV               <span class="dim">enhancement  carla    5d</span>

<span class="dim">enter open · w start work · c comment · x close · → pull requests · esc</span></pre>
      </div>
"""


SESSION_MOCK = """\
      <div class="term">
        <div class="term-chrome" aria-hidden="true">
          <span class="term-dots"><i></i><i></i><i></i></span>
          <span class="term-title">tend — work</span>
        </div>
        <div class="frame">
          <aside class="side">
            <p class="tools"><span>F</span><span class="on">A</span><span>S</span><span>I</span><span>B</span><span>C</span></p>
            <p class="rule" aria-hidden="true"></p>
            <p class="side-h">spaces</p>
            <p class="entry on"><span class="name"><span class="dot"></span>tend</span><span class="sub">main</span></p>
            <p class="entry"><span class="name"><span class="dot idle"></span>website</span><span class="sub">feat/hero</span></p>
            <p class="entry"><span class="name"><span class="dot idle"></span>docs</span><span class="sub">readme</span></p>
            <p class="rule" aria-hidden="true"></p>
            <p class="side-h">agents <span>flat</span></p>
            <p class="entry on"><span class="name"><span class="dot work"></span>claude</span><span class="sub">working</span></p>
            <p class="entry"><span class="name"><span class="dot block"></span>codex</span><span class="sub">blocked</span></p>
            <p class="entry"><span class="name"><span class="dot idle"></span>cursor</span><span class="sub">idle</span></p>
            <p class="hide" aria-hidden="true">«</p>
          </aside>
          <div class="workspace">
            <div class="tabs" aria-hidden="true">
              <span class="tab on">agents</span>
              <span class="tab">website</span>
              <span class="tab add">+</span>
              <span class="tabs-right">21:14</span>
            </div>
            <div class="panes">
              <article class="pane focus">
                <header><span class="id">1</span> claude <span class="st work" title="working">●</span></header>
                <pre><span class="logo"> ▐▛███▜▌
▝▜█████▛▘
  ▘▘ ▝▝</span>
<span class="dim">Claude Code</span>  <span class="accent">~/src/tend</span>

<span class="prompt">❯</span> keep the agents running after I detach

<span class="ok">●</span> The session is a background server.
  Closing this window does not stop them.

<span class="ok">●</span> Detach with ctrl+b d. Attach again
  and the same panes are there, still
  marked working, blocked or idle.

<span class="spin">⠋</span> <span class="work">Writing the status line…</span>
<span class="dim">   esc to interrupt</span></pre>
              </article>
              <article class="pane">
                <header><span class="id">2</span> codex <span class="st block" title="blocked">▲</span></header>
                <pre><span class="dim">codex</span>  <span class="accent">~/src/tend</span>

<span class="block">Do you want to proceed?</span>

  Run <span class="str">make check</span> before the commit

  <span class="dim">internal/ui/draw.go
  cmd/tend/tui.go</span>

<span class="prompt">❯</span> <span class="pick">1. Yes</span>
  2. No

<span class="dim">~/src/tend  feat/hero</span></pre>
              </article>
              <aside class="files" aria-label="Files panel">
                <p class="files-h">FILES <span>⚙</span></p>
                <p class="files-git">tend · main <span>⟳</span></p>
                <p class="rule" aria-hidden="true"></p>
                <pre> cmd/
   tend/
 internal/
   server/
   ui/
 <span class="accent">site/</span>
   index.html
   site.css
 AGENTS.md
 <span class="work">README.md  M</span></pre>
              </aside>
            </div>
          </div>
        </div>
        <p class="status"><span>work · tend · agents</span><span>1:claude ●</span><span>2:codex ▲</span></p>
      </div>
"""


# Runs in <head> so a first visit is redirected before the English page paints.
# The browser language is used only until the visitor picks one. That pick is
# written on the click itself (not in the deferred site.js), so a later load
# cannot bounce them back to the detected locale.
LANG_JS = """
(function(){
  if (location.protocol === 'file:') return;
  var KEY = 'tend-lang';
  var ok = {en:1, pt:1, es:1, ja:1, zh:1};

  function siteRoot() {
    var p = location.pathname.replace(/index\\.html$/, '');
    p = p.replace(/\\/(pt|es|ja|zh)\\/?$/, '/');
    if (p.slice(-1) !== '/') p += '/';
    return p;
  }

  function save(lang) {
    if (!ok[lang]) return;
    try { localStorage.setItem(KEY, lang); } catch (e) {}
    try {
      document.cookie = KEY + '=' + lang + '; path=' + siteRoot() + '; max-age=31536000; SameSite=Lax';
    } catch (e) {}
  }

  function read() {
    try {
      var v = localStorage.getItem(KEY);
      if (v && ok[v]) return v;
    } catch (e) {}
    try {
      var parts = document.cookie.split(';');
      for (var i = 0; i < parts.length; i++) {
        var s = parts[i].replace(/^\\s+/, '').split('=');
        if (s[0] === KEY && ok[s[1]]) return s[1];
      }
    } catch (e) {}
    return null;
  }

  document.addEventListener('click', function (ev) {
    var el = ev.target;
    while (el && el !== document) {
      if (el.getAttribute && el.getAttribute('data-lang') && el.className !== undefined) {
        var wrap = el.parentNode;
        if (wrap && wrap.className && (' ' + wrap.className + ' ').indexOf(' langs ') !== -1) {
          save(el.getAttribute('data-lang'));
          return;
        }
      }
      el = el.parentNode;
    }
  }, true);

  var path = location.pathname;
  var cur = 'en';
  var m = path.match(/\\/(pt|es|ja|zh)(?:\\/index\\.html|\\/)?$/);
  if (m) cur = m[1];
  var stored = read();
  function fromBrowser() {
    var list = navigator.languages || [navigator.language || navigator.userLanguage || ''];
    for (var i = 0; i < list.length; i++) {
      var c = String(list[i] || '').toLowerCase();
      if (c.indexOf('pt') === 0) return 'pt';
      if (c.indexOf('es') === 0) return 'es';
      if (c.indexOf('ja') === 0) return 'ja';
      if (c.indexOf('zh') === 0) return 'zh';
    }
    return 'en';
  }
  var want = stored || fromBrowser();
  if (want !== cur && (stored || cur === 'en')) {
    location.replace(want === 'en' ? siteRoot() : siteRoot() + want + '/');
  }
})();
""".strip()

BIN_LINUX = "https://github.com/auth-com-br/tend/releases/latest/download/tend-linux-amd64"
RELEASES = "https://github.com/auth-com-br/tend/releases/latest"


def page(code: str, t: dict) -> str:
    css = "site.css" if t["dir"] == "." else "../site.css"
    js = "site.js" if t["dir"] == "." else "../site.js"
    h = t["href"]

    def lang_link(key: str, label: str) -> str:
        current = ' aria-current="page"' if key == code else ""
        # Inline save so a language click is remembered even if site.js never runs.
        return (
            f'<a href="{h[key]}" data-lang="{key}"{current} '
            f"onclick=\"try{{localStorage.setItem('tend-lang','{key}')}}catch(e){{}}\">{label}</a>"
        )

    langs = " · ".join(
        [
            lang_link("pt", "pt"),
            lang_link("en", "en"),
            lang_link("es", "es"),
            lang_link("ja", "ja"),
            lang_link("zh", "zh"),
        ]
    )

    return f"""<!DOCTYPE html>
<html lang="{t["html_lang"]}" data-locale="{code}">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{t["title"]}</title>
  <meta name="description" content="{t["description"]}">
  <meta property="og:title" content="{t["title"]}">
  <meta property="og:description" content="{t["description"]}">
  <meta property="og:image" content="https://tend.auth.com.br/session.png">
  <meta name="twitter:card" content="summary_large_image">
  <link rel="alternate" hreflang="en" href="https://tend.auth.com.br/">
  <link rel="alternate" hreflang="pt-BR" href="https://tend.auth.com.br/pt/">
  <link rel="alternate" hreflang="es" href="https://tend.auth.com.br/es/">
  <link rel="alternate" hreflang="ja" href="https://tend.auth.com.br/ja/">
  <link rel="alternate" hreflang="zh-CN" href="https://tend.auth.com.br/zh/">
  <link rel="alternate" hreflang="zh" href="https://tend.auth.com.br/zh/">
  <link rel="alternate" hreflang="x-default" href="https://tend.auth.com.br/">
  <link rel="stylesheet" href="{css}">
  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
  <link href="https://fonts.googleapis.com/css2?family=IBM+Plex+Mono:wght@400;500;600&amp;display=swap" rel="stylesheet">
  <script>{LANG_JS}</script>
</head>
<body>
  <header class="bar">
    <a class="mark" href="#top">tend</a>
    <nav>
      <a href="#install">{t["nav_install"]}</a>
      <a href="#session">{t["nav_session"]}</a>
      <a href="#tools">{t["nav_tools"]}</a>
      <a href="#keys">{t["nav_keys"]}</a>
      <a href="https://github.com/auth-com-br/tend">{t["nav_source"]}</a>
      <span class="langs">{langs}</span>
    </nav>
  </header>

  <main id="top">
    <section class="lead">
      <h1>tend</h1>
      <p class="deck">{t["deck"]}</p>
    </section>

    <section id="install"
      data-copy="{t["copy"]}"
      data-copied="{t["copied"]}"
      data-detected-linux="{t["detected_linux"]}"
      data-detected-macos="{t["detected_macos"]}"
      data-detected-windows="{t["detected_windows"]}"
      data-download="{t["download_prefix"]}">
      <h2>{t["install_h"]} <span class="os-detected" id="os-detected" hidden></span></h2>
      <div class="os-tabs" role="tablist" aria-label="{t["os_tabs_label"]}">
        <button type="button" role="tab" id="tab-linux" data-os="linux" aria-selected="true" aria-controls="panel-unix">{t["os_linux"]}</button>
        <button type="button" role="tab" id="tab-darwin" data-os="darwin" aria-selected="false" aria-controls="panel-unix">{t["os_macos"]}</button>
        <button type="button" role="tab" id="tab-windows" data-os="windows" aria-selected="false" aria-controls="panel-windows">{t["os_windows"]}</button>
      </div>
      <div id="panel-unix" class="install-panel" role="tabpanel" data-panel="unix">
        <div class="cmd-wrap">
          <pre class="cmd" id="install-cmd">{INSTALL}</pre>
          <button type="button" class="copy" data-copy="{INSTALL}" data-label="{t["copy"]}" data-done="{t["copied"]}">{t["copy"]}</button>
        </div>
        <p class="install-dl">
          <a id="bin-link" href="{BIN_LINUX}">{t["download_prefix"]} tend-linux-amd64</a>
          <span class="sep">·</span>
          <a id="bin-alt" href="https://github.com/auth-com-br/tend/releases/latest/download/tend-linux-arm64">arm64</a>
          <span class="sep">·</span>
          <a href="{RELEASES}">{t["all_builds"]}</a>
        </p>
        <p>{t["install_p1"]}</p>
        <p class="note">{t["install_note"]}</p>
      </div>
      <div id="panel-windows" class="install-panel" role="tabpanel" data-panel="windows" hidden>
        <p>{t["windows_p1"]}</p>
        <p>{t["windows_p2"]}</p>
      </div>
    </section>

    <section id="session" class="stage" aria-label="{t["stage_label"]}">
{SESSION_MOCK}
      <p class="stage-cap">{t["stage_cap"]}</p>
    </section>

    <section class="usage">
      <p>{t["install_p2"]}</p>
      <p>{t["install_p3"]}</p>
    </section>

    <section class="facts">
      <h2>{t["facts_h"]}</h2>
      <dl>
        <div>
          <dt>{t["fact1_t"]}</dt>
          <dd>{t["fact1_d"]}</dd>
        </div>
        <div>
          <dt>{t["fact2_t"]}</dt>
          <dd>{t["fact2_d"]}</dd>
        </div>
        <div>
          <dt>{t["fact3_t"]}</dt>
          <dd>{t["fact3_d"]}</dd>
        </div>
      </dl>
    </section>

    <section id="tools" class="facts">
      <h2>{t["tools_h"]}</h2>
      <p class="note">{t["tools_note"]}</p>
      <dl>
        <div>
          <dt>{t["tool1_t"]}</dt>
          <dd>{t["tool1_d"]}</dd>
        </div>
        <div>
          <dt>{t["tool2_t"]}</dt>
          <dd>{t["tool2_d"]}</dd>
        </div>
        <div>
          <dt>{t["tool3_t"]}</dt>
          <dd>{t["tool3_d"]}</dd>
        </div>
        <div>
          <dt>{t["tool4_t"]}</dt>
          <dd>{t["tool4_d"]}</dd>
        </div>
        <div>
          <dt>{t["tool5_t"]}</dt>
          <dd>{t["tool5_d"]}</dd>
        </div>
        <div>
          <dt>{t["tool7_t"]}</dt>
          <dd>{t["tool7_d"]}</dd>
        </div>
        <div>
          <dt>{t["tool8_t"]}</dt>
          <dd>{t["tool8_d"]}</dd>
        </div>
        <div>
          <dt>{t["tool6_t"]}</dt>
          <dd>{t["tool6_d"]}</dd>
        </div>
      </dl>
      <div class="stage">
{ISSUES_MOCK}
        <p class="stage-cap">{t["issues_cap"]}</p>
      </div>
      <div class="stage">
{SESSIONS_MOCK}
        <p class="stage-cap">{t["sessions_cap"]}</p>
      </div>
    </section>

    <section id="keys">
      <h2>{t["keys_h"]}</h2>
      <p class="note">{t["keys_note"]}</p>
      <table>
        <tbody>
          <tr><th><kbd>|</kbd> <kbd>-</kbd></th><td>{t["k_split"]}</td></tr>
          <tr><th><kbd>h</kbd><kbd>j</kbd><kbd>k</kbd><kbd>l</kbd></th><td>{t["k_focus"]}</td></tr>
          <tr><th><kbd>H</kbd><kbd>J</kbd><kbd>K</kbd><kbd>L</kbd></th><td>{t["k_swap"]}</td></tr>
          <tr><th><kbd>r</kbd></th><td>{t["k_resize"]}</td></tr>
          <tr><th><kbd>z</kbd></th><td>{t["k_zoom"]}</td></tr>
          <tr><th><kbd>[</kbd></th><td>{t["k_scroll"]}</td></tr>
          <tr><th><kbd>x</kbd></th><td>{t["k_close"]}</td></tr>
          <tr><th><kbd>c</kbd> <kbd>n</kbd> <kbd>p</kbd> <kbd>1</kbd>–<kbd>9</kbd></th><td>{t["k_tabs"]}</td></tr>
          <tr><th><kbd>N</kbd> <kbd>(</kbd> <kbd>)</kbd></th><td>{t["k_spaces"]}</td></tr>
          <tr><th><kbd>a</kbd></th><td>{t["k_agents"]}</td></tr>
          <tr><th><kbd>g</kbd></th><td>{t["k_jump"]}</td></tr>
          <tr><th><kbd>d</kbd></th><td>{t["k_detach"]}</td></tr>
        </tbody>
      </table>
    </section>
  </main>

  <footer>
    <p>{t["made_by"]} <a href="https://auth.com.br">Auth Tecnologia Ltda</a> · Belo Horizonte, Brasil</p>
    <p>{t["footer"]}</p>
  </footer>
  <script src="{js}" defer></script>
</body>
</html>
"""


def main() -> None:
    for code, t in LANGS.items():
        html = page(code, t)
        if html.index('id="install"') > html.index('id="session"'):
            raise SystemExit("install section must sit above the session mock")
        if t["dir"] == ".":
            path = ROOT / "index.html"
        else:
            dest = ROOT / t["dir"]
            dest.mkdir(parents=True, exist_ok=True)
            path = dest / "index.html"
        path.write_text(html)
        print("wrote", path.relative_to(ROOT))


if __name__ == "__main__":
    main()

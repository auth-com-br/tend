// tend web: the agents of a session, on a phone. Two views — the list, and
// one agent's screen with a box to message it — over one event stream
// (/api/events) that carries the list whenever it changes and the screen of
// the agent shown. Actions are POSTs the server checks: a message, one of
// the keys an agent asks for, sleep and wake.
'use strict';

const $ = id => document.getElementById(id);
let agents = [];
let shown = 0; // the pane on screen, 0 on the list
let stream = null;
let previous = new Map(); // pane → state, to tell what changed
// A tap on a notification opens the page at /?pane=N: that agent is shown
// once the list says it is there.
let wanted = Number(new URLSearchParams(location.search).get('pane')) || 0;
if (wanted) history.replaceState(null, '', '/');

const STATE_WORDS = {
  blocked: 'needs you',
  working: 'working',
  idle: 'idle',
  asleep: 'asleep 💤',
  exited: 'exited',
  unknown: '',
};

function stateOf(a) {
  if (a.state === 'idle' && a.done) return 'done';
  return a.state;
}

function words(a) {
  return stateOf(a) === 'done' ? 'finished ✓' : STATE_WORDS[a.state] ?? a.state;
}

function el(tag, cls, text) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text !== undefined) e.textContent = text;
  return e;
}

function renderList() {
  const list = $('list');
  list.replaceChildren();
  if (agents.length === 0) {
    list.append(el('div', 'empty', 'No agents in this session yet.'));
    return;
  }
  let company = null;
  for (const a of agents) {
    const c = a.company || 'no company';
    if (c !== company) {
      company = c;
      list.append(el('div', 'company', c));
    }
    const card = el('button', 'card ' + stateOf(a));
    const top = el('div', 'top');
    top.append(el('span', 'name', a.agent), el('span', 'where', a.space), el('span', 'state', words(a)));
    card.append(top, el('div', 'about', a.title || a.tab || ''));
    card.addEventListener('click', () => open(a.pane));
    list.append(card);
  }
}

function current() {
  return agents.find(a => a.pane === shown);
}

function renderAgent() {
  const a = current();
  if (!a) return;
  $('title').textContent = `${a.agent} · ${words(a)}`;
  $('where').textContent = [a.company, a.space, a.tab].filter(Boolean).join(' › ') + (a.title ? ` — ${a.title}` : '');
  $('sleepwake').textContent = a.dormant ? 'wake' : 'sleep';
}

function show(view) {
  const onList = view === 'list';
  $('list').classList.toggle('hidden', !onList);
  $('agent').classList.toggle('hidden', onList);
  $('back').classList.toggle('hidden', onList);
  $('signout').classList.toggle('hidden', !onList);
  if (onList) $('title').textContent = 'tend';
}

function connect() {
  if (stream) stream.close();
  stream = new EventSource('/api/events' + (shown ? `?pane=${shown}` : ''));
  stream.onopen = () => $('conn').classList.add('on');
  stream.onerror = () => $('conn').classList.remove('on');
  stream.addEventListener('agents', e => {
    const next = JSON.parse(e.data) || [];
    tell(next);
    agents = next;
    if (wanted && agents.some(a => a.pane === wanted)) {
      const pane = wanted;
      wanted = 0;
      open(pane);
      return;
    }
    if (shown) {
      if (!current()) {
        back();
        return;
      }
      renderAgent();
    } else {
      renderList();
    }
  });
  stream.addEventListener('screen', e => {
    const m = JSON.parse(e.data);
    if (m.pane !== shown) return;
    const scr = $('screen');
    // Kept at the bottom, where an agent writes, unless the reader has
    // scrolled up to read.
    const atEnd = scr.scrollHeight - scr.scrollTop - scr.clientHeight < 40;
    scr.innerHTML = m.html; // drawn and escaped by the server
    if (atEnd) scr.scrollTop = scr.scrollHeight;
  });
}

// tell: what changed since the list before — an agent that now needs the
// person, or that finished — buzzes and shows, as the TUI notifies.
function tell(next) {
  for (const a of next) {
    const was = previous.get(a.pane);
    const now = stateOf(a);
    if (was !== undefined && was !== now) {
      if (now === 'blocked') {
        notice(`${a.agent} in ${a.space} needs you`, [200, 100, 200]);
      } else if (now === 'done' && was === 'working') {
        notice(`${a.agent} in ${a.space} finished`, [120]);
      }
    }
  }
  previous = new Map(next.map(a => [a.pane, stateOf(a)]));
  const waiting = next.filter(a => a.state === 'blocked').length;
  document.title = waiting ? `(${waiting}) tend` : 'tend';
}

function notice(text, pattern) {
  if (navigator.vibrate) navigator.vibrate(pattern);
  toast(text);
}

function toast(text) {
  const t = el('div', 'toast', text);
  document.body.append(t);
  setTimeout(() => t.remove(), 3500);
}

function open(pane) {
  shown = pane;
  $('screen').textContent = '';
  show('agent');
  renderAgent();
  history.pushState({ pane }, '');
  connect();
}

function back() {
  shown = 0;
  show('list');
  renderList();
  connect();
}

async function act(path, body) {
  try {
    const r = await fetch(path, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ pane: shown, ...body }),
    });
    if (!r.ok) {
      toast((await r.text()).trim() || `failed (${r.status})`);
      return null;
    }
    return r.json();
  } catch (err) {
    toast('not connected');
    return null;
  }
}

$('back').addEventListener('click', () => history.back());
$('signout').addEventListener('click', async () => {
  if (!confirm('Sign this phone out of tend?')) return;
  await fetch('/logout', { method: 'POST' });
  location.replace('/');
});
window.addEventListener('popstate', () => {
  if (shown) back();
});

$('send').addEventListener('submit', async e => {
  e.preventDefault();
  const box = $('message');
  const text = box.value;
  if (!text.trim()) return;
  box.value = '';
  box.style.height = '';
  if (!(await act('/api/prompt', { text }))) box.value = text;
});

$('message').addEventListener('input', e => {
  const box = e.target;
  box.style.height = '';
  box.style.height = Math.min(box.scrollHeight, 160) + 'px';
});

for (const b of document.querySelectorAll('#keys button[data-key]')) {
  b.addEventListener('click', () => act('/api/key', { key: b.dataset.key }));
}

$('sleepwake').addEventListener('click', async () => {
  const a = current();
  if (!a) return;
  if (a.dormant) {
    if (await act('/api/wake', {})) toast(`waking ${a.agent}…`);
  } else {
    const r = await act('/api/sleep', {});
    if (r) {
      const mb = r.freed ? `, ${Math.round(r.freed / 1048576)} MB given back` : '';
      toast(`${a.agent} is asleep${mb}`);
    }
  }
});

// Notifications with the page closed (push.go): offered only on a secure
// page — the browser allows them nowhere else — and when the server has them
// on ([web] https). The bell says whether this phone is told.
function serverKey(b64) {
  const s = atob(b64.replace(/-/g, '+').replace(/_/g, '/') + '='.repeat((4 - (b64.length % 4)) % 4));
  return Uint8Array.from(s, c => c.charCodeAt(0));
}

async function postJSON(path, body) {
  return fetch(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
}

async function setupPush() {
  if (!window.isSecureContext || !('serviceWorker' in navigator) || !('PushManager' in window)) return;
  let info;
  try {
    info = await (await fetch('/api/push')).json();
  } catch {
    return;
  }
  if (!info.available) return;
  const reg = await navigator.serviceWorker.register('/sw.js');
  let sub = await reg.pushManager.getSubscription();
  // The server may have lost it (a new key, a cleared state folder): saying
  // it again costs nothing and keeps the two in step.
  if (sub) postJSON('/api/push/subscribe', sub.toJSON()).catch(() => {});
  const bell = $('notify');
  const paint = () => {
    bell.textContent = sub ? '🔔' : '🔕';
    bell.title = sub ? 'this phone is told when an agent needs you — tap to stop' : 'notify this phone when an agent needs you';
  };
  paint();
  bell.classList.remove('hidden');
  bell.addEventListener('click', async () => {
    if (sub) {
      await postJSON('/api/push/unsubscribe', { endpoint: sub.endpoint }).catch(() => {});
      await sub.unsubscribe();
      sub = null;
      paint();
      toast('notifications off on this phone');
      return;
    }
    if ((await Notification.requestPermission()) !== 'granted') {
      toast('notifications are blocked for this page in the browser settings');
      return;
    }
    try {
      sub = await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: serverKey(info.key) });
    } catch (err) {
      toast('could not subscribe: ' + err.message);
      return;
    }
    const r = await postJSON('/api/push/subscribe', sub.toJSON());
    if (!r.ok) {
      toast((await r.text()).trim() || `failed (${r.status})`);
      return;
    }
    paint();
    toast('this phone will be told when an agent needs you');
  });
}

show('list');
connect();
setupPush();

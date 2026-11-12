'use strict';
// SecureSpeak client UI. Talks to the local Go core: commands via POST /api,
// events via Server-Sent Events. All user-provided text is rendered with
// textContent, never as HTML.

const TOKEN = new URLSearchParams(location.search).get('token') || '';

// ---------------------------------------------------------------- icons
const P = {
  mic: '<rect x="9" y="2" width="6" height="12" rx="3"/><path d="M19 10v1a7 7 0 0 1-14 0v-1M12 18v4"/>',
  micOff: '<rect x="9" y="2" width="6" height="12" rx="3"/><path d="M19 10v1a7 7 0 0 1-14 0v-1M12 18v4M3 3l18 18"/>',
  head: '<path d="M3 18v-6a9 9 0 0 1 18 0v6"/><path d="M21 19a2 2 0 0 1-2 2h-1v-6h3zM3 19a2 2 0 0 0 2 2h1v-6H3z"/>',
  headOff: '<path d="M3 18v-6a9 9 0 0 1 18 0v6"/><path d="M21 19a2 2 0 0 1-2 2h-1v-6h3zM3 19a2 2 0 0 0 2 2h1v-6H3zM3 3l18 18"/>',
  moon: '<path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z"/>',
  plug: '<path d="M9 2v6M15 2v6M6 8h12v4a6 6 0 0 1-12 0zM12 18v4"/>',
  unplug: '<path d="M9 2v6M15 2v6M6 8h12v4a6 6 0 0 1-12 0zM12 18v4M3 3l18 18"/>',
  sliders: '<path d="M4 21v-7M4 10V3M12 21v-9M12 8V3M20 21v-5M20 12V3M1 14h6M9 8h6M17 16h6"/>',
  shield: '<path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/>',
  shieldCheck: '<path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/><path d="M9 12l2 2 4-4"/>',
  lock: '<rect x="4" y="11" width="16" height="10" rx="2"/><path d="M8 11V7a4 4 0 0 1 8 0v4"/>',
  plus: '<path d="M12 5v14M5 12h14"/>',
  hash: '<path d="M4 9h16M4 15h16M10 3L8 21M16 3l-2 18"/>',
  user: '<circle cx="12" cy="8" r="4"/><path d="M4 21a8 8 0 0 1 16 0"/>',
  x: '<path d="M18 6L6 18M6 6l12 12"/>',
  send: '<path d="M22 2L11 13M22 2l-7 20-4-9-9-4z"/>',
  alert: '<path d="M12 9v4M12 17h.01"/><path d="M10.3 3.9L1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0z"/>',
  bell: '<path d="M18 8a6 6 0 0 0-12 0c0 7-3 9-3 9h18s-3-2-3-9M13.7 21a2 2 0 0 1-3.4 0"/>',
  msg: '<path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/>',
  vol: '<path d="M11 5L6 9H2v6h4l5 4zM15.5 8.5a5 5 0 0 1 0 7M19 5a10 10 0 0 1 0 14"/>',
  volX: '<path d="M11 5L6 9H2v6h4l5 4zM23 9l-6 6M17 9l6 6"/>',
  ban: '<circle cx="12" cy="12" r="10"/><path d="M4.9 4.9l14.2 14.2"/>',
  kick: '<path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4M16 17l5-5-5-5M21 12H9"/>',
  enter: '<path d="M15 3h4a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2h-4M10 17l5-5-5-5M15 12H3"/>',
  edit: '<path d="M12 20h9M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4z"/>',
  trash: '<path d="M3 6h18M8 6V4h8v2M19 6l-1 14H6L5 6"/>',
  key: '<circle cx="7.5" cy="15.5" r="5.5"/><path d="M21 2l-9.6 9.6M15.5 7.5l3 3L22 7l-3-3"/>',
  server: '<rect x="2" y="3" width="20" height="8" rx="2"/><rect x="2" y="13" width="20" height="8" rx="2"/><path d="M6 7h.01M6 17h.01"/>',
  star: '<path d="M12 2l3.1 6.3 6.9 1-5 4.9 1.2 6.8L12 17.8 5.8 21l1.2-6.8-5-4.9 6.9-1z"/>',
  radio: '<circle cx="12" cy="12" r="2"/><path d="M16.2 7.8a6 6 0 0 1 0 8.5M7.8 16.2a6 6 0 0 1 0-8.5M19.1 4.9a10 10 0 0 1 0 14.2M4.9 19.1a10 10 0 0 1 0-14.2"/>',
  list: '<path d="M8 6h13M8 12h13M8 18h13M3 6h.01M3 12h.01M3 18h.01"/>',
  folderPlus: '<path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2zM12 11v6M9 14h6"/>',
  move: '<path d="M5 12h14M12 5l7 7-7 7"/>',
  clip: '<path d="M21.4 11.1l-9.2 9.2a6 6 0 0 1-8.5-8.5l9.2-9.2a4 4 0 0 1 5.7 5.7l-9.2 9.2a2 2 0 0 1-2.8-2.8l8.5-8.5"/>',
  file: '<path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><path d="M14 2v6h6"/>',
  download: '<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4M7 10l5 5 5-5M12 15V3"/>',
  folder: '<path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/>',
  whisper: '<path d="M3 12h4l3-8 4 16 3-8h4"/>',
  image: '<rect x="3" y="3" width="18" height="18" rx="2"/><circle cx="8.5" cy="8.5" r="1.5"/><path d="M21 15l-5-5L5 21"/>',
  monitor: '<rect x="2" y="3" width="20" height="14" rx="2"/><path d="M8 21h8M12 17v4"/>',
  expand: '<path d="M15 3h6v6M9 21H3v-6M21 3l-7 7M3 21l7-7"/>',
  globe: '<circle cx="12" cy="12" r="10"/><path d="M2 12h20M12 2a15 15 0 0 1 0 20 15 15 0 0 1 0-20z"/>',
};
function icon(name) {
  const s = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  s.setAttribute('viewBox', '0 0 24 24');
  s.setAttribute('fill', 'none');
  s.setAttribute('stroke', 'currentColor');
  s.setAttribute('stroke-width', '2');
  s.setAttribute('stroke-linecap', 'round');
  s.setAttribute('stroke-linejoin', 'round');
  s.innerHTML = P[name] || ''; // static, trusted markup
  return s;
}

// ---------------------------------------------------------------- dom helpers
function el(tag, attrs, ...kids) {
  const e = document.createElement(tag);
  if (attrs) for (const [k, v] of Object.entries(attrs)) {
    if (v == null || v === false) continue;
    if (k === 'class') e.className = v;
    else if (k === 'text') e.textContent = v;
    else if (k.startsWith('on')) e.addEventListener(k.slice(2), v);
    else if (k === 'style') e.style.cssText = v;
    else if (k in e && typeof v !== 'string') e[k] = v;
    else e.setAttribute(k, v === true ? '' : v);
  }
  for (const k of kids.flat()) if (k != null && k !== false) e.append(k instanceof Node ? k : document.createTextNode(String(k)));
  return e;
}
const $ = (id) => document.getElementById(id);
// append, skipping false/null/undefined (from `cond && el(...)` patterns)
function add(parent, ...kids) { for (const k of kids.flat()) if (k != null && k !== false && k !== '') parent.append(k); }
function btnContent(b, ic, label) { b.replaceChildren(icon(ic), label ? el('span', { text: label }) : ''); }

// ---------------------------------------------------------------- api
async function api(cmd, args = {}) {
  const r = await fetch('/api', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-Token': TOKEN },
    body: JSON.stringify({ cmd, args }),
  });
  const j = await r.json();
  if (!j.ok) throw new Error(j.error || 'error');
  return j.result;
}
async function act(cmd, args) {
  try { return await api(cmd, args); } catch (e) { toast(e.message, 'error'); return undefined; }
}

// ---------------------------------------------------------------- state
let S = null;
let selected = null; // {type:'server'|'channel'|'client', id}
let talking = new Set();
let whispering = new Set();
let selfWhisper = false;
const fileStates = new Map(); // file id -> {in: {...}, out: Map(peerId -> {...})}
let stats = {};
let level = { db: -90, tx: false };
const tabs = new Map();
let activeTab = 'server';
let lastChannel = 0;
let settingsView = null; // live-updating settings modal hooks
let bansView = null;

function ensureTab(key, title, closable) {
  if (!tabs.has(key)) tabs.set(key, { title, msgs: [], unread: 0, closable });
  else if (title) tabs.get(key).title = title;
  return tabs.get(key);
}
ensureTab('server', 'Server', false);
ensureTab('channel', 'Channel', false);

const chanById = (id) => S && S.channels.find((c) => c.id === id);
const userById = (id) => S && S.clients.find((c) => c.id === id);
const can = (p) => !!(S && S.connected && S.perms && S.perms.includes(p));
// moderation of another user needs more power than theirs; the owner is untouchable
const canAct = (u) => !!(S && S.connected && !u.me && (u.owner ? false : (S.owner || S.power > u.power)));
const canEdit = (ch) => S && (can('channel.edit') || (!ch.perm && ch.owner === S.myID));
const canDelete = (ch) => S && (can('channel.delete') || (!ch.perm && ch.owner === S.myID));
const roleById = (id) => S && (S.roles || []).find((r) => r.id === id);
const safeColor = (c) => (/^#[0-9a-f]{6}$/i.test(c || '') ? c : '');
const staffPerms = ['roles.manage', 'roles.assign', 'tokens.create', 'ban.list', 'server.edit', 'server.password'];
function roleBadges(u, max = 2) {
  const out = [];
  if (u.owner) out.push(el('span', { class: 'badge owner', text: 'OWNER', title: 'Server owner' }));
  const rs = (u.roles || []).map(roleById).filter(Boolean).sort((a, b) => b.power - a.power);
  for (const r of rs.slice(0, u.owner ? max - 1 : max)) {
    const b = el('span', { class: 'badge role', text: r.name.toUpperCase(), title: r.name + ' (power ' + r.power + ')' });
    const c = safeColor(r.color);
    if (c) b.style.cssText = 'color:' + c + ';background:' + c + '24';
    out.push(b);
  }
  if (rs.length > max) out.push(el('span', { class: 'badge', text: '+' + (rs.length - max) }));
  return out;
}

// ---------------------------------------------------------------- events
function startEvents() {
  const es = new EventSource('/events?token=' + encodeURIComponent(TOKEN));
  es.onmessage = (e) => {
    try { handle(JSON.parse(e.data)); } catch (err) { console.error(err); }
  };
  es.onerror = () => {
    $('st-conn').replaceChildren(el('span', { class: 'dot busy' }), 'Lost connection to SecureSpeak core…');
  };
}

function handle({ t, d }) {
  switch (t) {
    case 'state': onState(d); break;
    case 'talking': talking = new Set(d.ids); whispering = new Set(d.whisper); selfWhisper = d.selfWhisper; updateTalking(); break;
    case 'keys': AE.pttKey = d.ptt; AE.whisperKey = d.whisper; break;
    case 'sound': AE.sound(d); break;
    case 'file': onFileEvent(d); break;
    case 'token': showNewToken(d); break;
    case 'keyframe': SS.requestKeyframe(); break;
    case 'tokens': if (roleMgr) roleMgr.tokens(d.tokens || []); break;
    case 'members': if (roleMgr) roleMgr.members(d || []); break;
    case 'chat': addChat(d); break;
    case 'poke': showPoke(d); break;
    case 'error': toast(d.msg, 'error'); break;
    case 'stats': stats = d; renderStatus(); break;
    case 'bans': if (bansView) bansView(d.bans || []); break;
    case 'connected':
      for (const k of [...tabs.keys()]) if (k.startsWith('pm:')) tabs.delete(k);
      tabs.get('channel').msgs = [];
      addDivider('server', 'Connected to ' + d.server);
      selected = null;
      break;
    case 'disconnected':
      addDivider('server', d.reason || 'Disconnected');
      if (d.reason && d.reason !== 'Disconnected') toast(d.reason, 'warn');
      stats = {};
      break;
  }
}

function onState(d) {
  const prev = S;
  S = d;
  if (S.connected && S.myChannel !== lastChannel) {
    const ch = chanById(S.myChannel);
    if (ch) {
      tabs.get('channel').title = ch.name;
      addDivider('channel', 'You joined ' + ch.name);
    }
    lastChannel = S.myChannel;
  }
  if (!S.connected) lastChannel = 0;
  if (selected && selected.type === 'client' && !userById(selected.id)) selected = null;
  if (selected && selected.type === 'channel' && !chanById(selected.id)) selected = null;
  renderAll();
  AE.apply(S);
  if (settingsView) settingsView.refresh();
  if (roleMgr) roleMgr.refresh();
  // screen share bookkeeping
  if (!S.connected && SS.sharing) SS.stop();
  if (SS.watching) {
    const w = userById(SS.watching);
    if (!S.connected || !w || !w.sharing || w.channel !== S.myChannel) {
      if (w && S.connected) toast(w.nick + ' stopped sharing');
      SS.unwatch(true);
    }
  }
  renderShareButton();
  renderStage();
  if (!prev && !S.connected && !S.connecting) openConnect();
}

// ---------------------------------------------------------------- rendering
function renderAll() {
  renderToolbar();
  renderTree();
  renderInfo();
  renderTabs();
  renderStatus();
}

function renderToolbar() {
  const c = S.connected;
  btnContent($('btn-connect'), 'plug', S.connecting ? 'Connecting…' : 'Connect');
  $('btn-connect').disabled = c || S.connecting;
  btnContent($('btn-disconnect'), 'unplug', 'Disconnect');
  $('btn-disconnect').disabled = !c;
  btnContent($('btn-mute'), S.muted ? 'micOff' : 'mic');
  $('btn-mute').classList.toggle('active', S.muted);
  btnContent($('btn-deaf'), S.deaf ? 'headOff' : 'head');
  $('btn-deaf').classList.toggle('active', S.deaf);
  btnContent($('btn-away'), 'moon');
  $('btn-away').classList.toggle('active', S.away);
  btnContent($('btn-ptt'), 'radio', 'Push to talk');
  $('btn-ptt').classList.toggle('hidden', S.audio.mode !== 'ptt');
  renderWhisperBar();
  $('btn-attach').replaceChildren(icon('clip'));
  $('btn-attach').disabled = !c || activeTab === 'server';
  const staff = S.owner || staffPerms.some(can);
  btnContent($('btn-admin'), staff ? 'star' : 'key', staff ? 'Admin' : 'Privilege key');
  $('btn-admin').disabled = !c;
  $('btn-admin').title = staff ? 'Server administration' : 'Redeem a privilege key';
  btnContent($('btn-settings'), 'sliders', 'Settings');
  $('btn-new-channel').replaceChildren(icon('folderPlus'));
  $('btn-new-channel').disabled = !c;
  $('server-title').textContent = c ? S.serverName : (S.connecting ? 'Connecting…' : 'Not connected');
  $('mic-thresh').style.display = S.audio.mode === 'va' ? '' : 'none';
  $('mic-thresh').style.left = dbToPct(S.audio.vaThreshold) + '%';
  const input = $('chat-input');
  input.disabled = !c;
  $('chat-form').querySelector('.send').disabled = !c;
  $('chat-form').querySelector('.send').replaceChildren(icon('send'));
}

function dbToPct(db) { return Math.max(0, Math.min(100, ((db + 70) / 70) * 100)); }

function updateLevel() {
  const l = $('mic-level');
  l.style.width = dbToPct(level.db) + '%';
  l.classList.toggle('tx', level.tx);
  l.classList.toggle('whisper', level.tx && level.mode > 0);
  if (settingsView) settingsView.level(level);
}

let levelFrame = false;
AE.onLevel = (db, tx, mode) => {
  level = { db, tx, mode };
  if (!levelFrame) { levelFrame = true; requestAnimationFrame(() => { levelFrame = false; updateLevel(); }); }
};
AE.onStatus = () => { renderStatus(); if (settingsView) settingsView.refresh(); };

function updateTalking() {
  document.querySelectorAll('.user[data-id]').forEach((n) => {
    const id = +n.dataset.id;
    n.classList.toggle('talking', talking.has(id));
    n.classList.toggle('whispering', whispering.has(id) || (S && id === S.myID && selfWhisper && talking.has(id)));
  });
  $('btn-ptt').classList.toggle('down', S && talking.has(S.myID));
  const w = [...whispering].map((id) => userById(id)).filter(Boolean).map((u) => u.nick);
  $('st-whisper').textContent = w.length ? w.join(', ') + (w.length > 1 ? ' are' : ' is') + ' whispering to you' : '';
}

function liveBadge(u) {
  const watchable = !u.me && u.channel === S.myChannel;
  const b = el('span', { class: 'badge live' + (watchable ? ' click' : ''), text: SS.watching === u.id ? 'WATCHING' : 'LIVE',
    title: u.me ? 'You are sharing your screen' : watchable ? 'Watch ' + u.nick + '’s screen' : 'Join their channel to watch' });
  if (watchable) b.onclick = (e) => { e.stopPropagation(); watchUser(u); };
  return b;
}

function avatarNode(u, size) {
  const box = el('span', { class: 'avatar' + (size ? ' big' : '') });
  if (u.avatar) {
    const img = el('img', { alt: '', src: '/avatar?id=' + (u.me ? 0 : u.id) + '&h=' + u.avatar + '&token=' + encodeURIComponent(TOKEN) });
    img.onerror = () => img.remove();
    box.append(img);
  } else {
    box.append(icon(u.deaf ? 'headOff' : (u.muted || u.serverMuted) ? 'micOff' : 'user'));
  }
  return box;
}

function renderTree() {
  const tree = $('tree');
  const scroll = tree.scrollTop;
  tree.replaceChildren();
  if (!S.connected) {
    tree.append(el('div', { class: 'empty' },
      el('div', { class: 'big' }, icon('server')),
      el('div', { text: S.connecting ? 'Connecting…' : 'Not connected to a server' }),
      !S.connecting && el('div', { style: 'margin-top:14px' }, el('button', { class: 'btn primary', onclick: openConnect }, icon('plug'), 'Connect'))));
    return;
  }
  const root = el('div', { class: 'row' + (selected?.type === 'server' ? ' selected' : '') },
    el('span', { class: 'ico' }, icon('server')), el('span', { class: 'name', text: S.serverName, style: 'font-weight:600' }),
    el('span', { class: 'tail', text: S.clients.length + (S.maxClients ? ' / ' + S.maxClients : '') }));
  root.onclick = () => select({ type: 'server' });
  root.oncontextmenu = (e) => { e.preventDefault(); select({ type: 'server' }); serverMenu(e); };
  tree.append(root);
  const byParent = new Map();
  for (const ch of S.channels) {
    if (!byParent.has(ch.parent)) byParent.set(ch.parent, []);
    byParent.get(ch.parent).push(ch);
  }
  const build = (parent, depth) => {
    for (const ch of byParent.get(parent) || []) {
      tree.append(channelNode(ch, depth));
      for (const u of S.clients.filter((x) => x.channel === ch.id)) tree.append(userNode(u, depth + 1));
      build(ch.id, depth + 1);
    }
  };
  build(0, 1);
  tree.scrollTop = scroll;
}

function indent(depth) { return 'padding-left:' + (8 + depth * 18) + 'px'; }

function channelNode(ch, depth) {
  const count = S.clients.filter((x) => x.channel === ch.id).length;
  const wrap = el('div', { class: 'chan' + (ch.id === S.myChannel ? ' mine' : '') + (ch.pw ? ' locked' : '') });
  const row = el('div', { class: 'row' + (selected?.type === 'channel' && selected.id === ch.id ? ' selected' : ''), style: indent(depth) },
    el('span', { class: 'ico' }, icon(ch.pw ? 'lock' : 'hash')),
    el('span', { class: 'name', text: ch.name }),
    ch.topic && el('span', { class: 'sub', text: '— ' + ch.topic }),
    el('span', { class: 'tail' },
      !ch.perm && el('span', { class: 'badge', text: 'TEMP' }),
      ch.def && el('span', { class: 'badge', text: 'DEFAULT' }),
      ch.max ? `${count}/${ch.max}` : ''));
  row.onclick = () => select({ type: 'channel', id: ch.id });
  row.ondblclick = () => joinChannel(ch);
  row.oncontextmenu = (e) => { e.preventDefault(); select({ type: 'channel', id: ch.id }); channelMenu(e, ch); };
  row.ondragover = (e) => { if (dragUser != null) { e.preventDefault(); row.classList.add('drop'); } };
  row.ondragleave = () => row.classList.remove('drop');
  row.ondrop = (e) => {
    e.preventDefault(); row.classList.remove('drop');
    const u = userById(dragUser);
    dragUser = null;
    if (!u || u.channel === ch.id) return;
    if (u.me) joinChannel(ch); else if (can('client.move') && canAct(u)) act('move', { id: u.id, channel: ch.id });
  };
  wrap.append(row);
  return wrap;
}

let dragUser = null;

function userNode(u, depth) {
  const cls = ['row', 'user'];
  if (u.me) cls.push('me');
  if (u.muted || u.serverMuted) cls.push('muted');
  if (u.deaf) cls.push('deaf');
  if (u.away) cls.push('away');
  if (talking.has(u.id)) cls.push('talking');
  if (whispering.has(u.id)) cls.push('whispering');
  if (selected?.type === 'client' && selected.id === u.id) cls.push('selected');
  const row = el('div', { class: cls.join(' '), 'data-id': u.id, style: indent(depth), draggable: (u.me || (can('client.move') && canAct(u))) ? 'true' : null },
    avatarNode(u),
    el('span', { class: 'name', text: u.nick }),
    (u.muted || u.serverMuted || u.deaf) && u.avatar && el('span', { class: 'st-icons' }, icon(u.deaf ? 'headOff' : 'micOff')),
    u.sharing && liveBadge(u),
    u.away && el('span', { class: 'sub', text: u.awayMsg ? '(away: ' + u.awayMsg + ')' : '(away)' }),
    el('span', { class: 'tail' },
      u.serverMuted && el('span', { class: 'badge bad', text: 'SERVER-MUTED' }),
      u.localMute && el('span', { class: 'badge', text: 'MUTED BY YOU' }),
      ...roleBadges(u),
      !u.me && !u.valid && el('span', { class: 'badge bad', text: 'INVALID KEY' }),
      !u.me && u.keyChanged && !u.verified && el('span', { class: 'badge warn', text: 'KEY CHANGED' }),
      !u.me && u.verified && el('span', { class: 'badge verified', title: 'Identity verified by you' }, '✓ VERIFIED')));
  row.onclick = () => select({ type: 'client', id: u.id });
  row.ondblclick = () => { if (!u.me) openPM(u); };
  row.oncontextmenu = (e) => { e.preventDefault(); select({ type: 'client', id: u.id }); userMenu(e, u); };
  row.ondragstart = (e) => { dragUser = u.id; e.dataTransfer.effectAllowed = 'move'; e.dataTransfer.setData('text/plain', String(u.id)); };
  row.ondragend = () => { dragUser = null; };
  return row;
}

function select(sel) {
  selected = sel;
  renderTree();
  renderInfo();
}

function kv(pairs) {
  return el('div', { class: 'kv' }, pairs.filter(Boolean).flatMap(([k, v]) => [el('span', { class: 'k', text: k }), v instanceof Node ? v : el('span', { text: String(v) })]));
}

function renderInfo() {
  const box = $('info');
  if (box.contains(document.activeElement) && document.activeElement.type === 'range') return; // don't disturb a drag
  box.replaceChildren();
  if (!S.connected) {
    add(box, 
      el('div', { class: 'info-title' }, icon('shieldCheck'), 'SecureSpeak'),
      el('div', { class: 'info-sub', text: 'Voice and text chat with end-to-end AES-256-GCM encryption. The server only relays ciphertext — it never has your keys.' }),
      el('div', { class: 'k', style: 'color:var(--faint);font-size:12px;margin-top:6px', text: 'Your identity fingerprint' }),
      el('div', { class: 'fp', text: S.identity }),
      el('div', { class: 'actions' }, el('button', { class: 'btn primary', onclick: openConnect }, icon('plug'), 'Connect to a server')));
    return;
  }
  const sel = selected || { type: 'channel', id: S.myChannel };
  if (sel.type === 'server') return renderServerInfo(box);
  if (sel.type === 'channel') {
    const ch = chanById(sel.id);
    if (ch) return renderChannelInfo(box, ch);
  }
  if (sel.type === 'client') {
    const u = userById(sel.id);
    if (u) return renderUserInfo(box, u);
  }
}

function renderServerInfo(box) {
  add(box, 
    el('div', { class: 'info-title' }, icon('server'), S.serverName),
    S.welcomeMsg && el('div', { class: 'info-sub', text: S.welcomeMsg }),
    kv([['Address', S.addr], ['Users', S.clients.length + (S.maxClients ? ' / ' + S.maxClients : '')], ['Transport', stats.transport || '…'], ['Password', S.serverHasPw ? 'Required to connect' : 'None'], ['Encryption', 'TLS 1.3 + end-to-end AES-256-GCM']]),
    el('div', { class: 'k', style: 'color:var(--faint);font-size:12px', text: 'Server certificate (SHA-256)' }),
    el('div', { class: 'fp', style: 'font-size:11px', text: S.certFP }),
    el('div', { class: 'actions' },
      can('server.edit') && el('button', { class: 'btn', onclick: openServerEdit }, icon('edit'), 'Edit server'),
      can('server.password') && el('button', { class: 'btn', onclick: openServerPassword }, icon('lock'), 'Server password'),
      (can('roles.manage') || can('roles.assign') || can('tokens.create')) && el('button', { class: 'btn', onclick: () => openRoleManager() }, icon('shield'), 'Roles & permissions'),
      can('ban.list') && el('button', { class: 'btn', onclick: openBans }, icon('list'), 'Ban list'),
      el('button', { class: 'btn', onclick: () => openChannelModal(null, 0) }, icon('folderPlus'), 'New channel')));
}

function renderChannelInfo(box, ch) {
  const users = S.clients.filter((x) => x.channel === ch.id).length;
  add(box, 
    el('div', { class: 'info-title' }, icon(ch.pw ? 'lock' : 'hash'), ch.name),
    ch.topic && el('div', { class: 'info-sub', style: 'color:var(--text)', text: ch.topic }),
    ch.desc && el('div', { class: 'info-sub', text: ch.desc }),
    kv([['Type', ch.perm ? 'Permanent' : 'Temporary (deleted when empty)'], ['Users', users + (ch.max ? ' / ' + ch.max : '')], ['Password', ch.pw ? 'Yes' : 'No'], ch.def && ['Default', 'New users land here']]),
    el('div', { class: 'actions' },
      ch.id !== S.myChannel && el('button', { class: 'btn primary', onclick: () => joinChannel(ch) }, icon('enter'), 'Join'),
      el('button', { class: 'btn', onclick: () => openChannelModal(null, ch.id) }, icon('folderPlus'), 'Sub-channel'),
      canEdit(ch) && el('button', { class: 'btn', onclick: () => openChannelModal(ch) }, icon('edit'), 'Edit'),
      canDelete(ch) && !ch.def && el('button', { class: 'btn danger', onclick: () => deleteChannel(ch) }, icon('trash'), 'Delete')));
}

function renderUserInfo(box, u) {
  const statusText = [u.muted && 'microphone muted', u.deaf && 'speakers muted', u.serverMuted && 'muted by server', u.away && ('away' + (u.awayMsg ? ': ' + u.awayMsg : ''))].filter(Boolean).join(', ') || 'online';
  add(box, el('div', { class: 'info-title' }, avatarNode(u, true), u.nick,
    ...roleBadges(u, 4),
    !u.me && u.verified && el('span', { class: 'badge verified', text: '✓ VERIFIED' })));
  add(box, el('div', { class: 'info-sub', text: (u.me ? 'This is you · ' : '') + statusText }));
  add(box, el('div', { class: 'k', style: 'color:var(--faint);font-size:12px', text: 'Identity fingerprint' }), el('div', { class: 'fp', text: u.fp }));
  if (u.me) {
    add(box, el('div', { class: 'actions' },
      el('button', { class: 'btn', onclick: changeNick }, icon('edit'), 'Change nickname'),
      el('button', { class: 'btn', onclick: () => toggleAway() }, icon('moon'), S.away ? 'Back' : 'Set away'),
      el('button', { class: 'btn', onclick: chooseAvatar }, icon('image'), 'Change picture')));
    return;
  }
  if (!u.valid) add(box, el('div', { class: 'callout danger', text: 'This user\'s session key is not signed by their identity. Nothing is encrypted to or accepted from them.' }));
  else if (u.keyChanged && !u.verified) add(box, el('div', { class: 'callout warn', text: 'This nickname used a different identity key last time on this server. Compare fingerprints before trusting this person.' }));
  else if (!u.verified) add(box, el('div', { class: 'callout', text: 'Not verified yet. Compare this fingerprint with ' + u.nick + ' over another channel (in person, phone) to rule out a man-in-the-middle.' }));
  else add(box, el('div', { class: 'callout ok', text: 'You verified this identity. Encryption to this user cannot be intercepted by the server.' }));
  if (u.channel === S.myChannel && !u.hasKey) add(box, el('div', { class: 'callout', text: 'Waiting for this user\'s voice key…' }));

  const val = el('span', { class: 'val', text: Math.round(u.volume * 100) + '%' });
  const slider = el('input', { type: 'range', min: 0, max: 300, step: 5, value: Math.round(u.volume * 100) });
  slider.oninput = () => { val.textContent = slider.value + '%'; act('peer_volume', { id: u.id, volume: slider.value / 100 }); };
  slider.onchange = () => slider.blur();
  add(box, el('div', { class: 'slider-row' }, el('label', { text: 'Volume' }), slider, val));
  if (u.sharing && u.channel === S.myChannel) add(box, el('div', { class: 'actions' },
    el('button', { class: 'btn primary', onclick: () => watchUser(u) }, icon('monitor'), 'Watch ' + u.nick + '’s screen')));
  add(box, el('div', { class: 'actions' },
    el('button', { class: 'btn', onclick: () => openPM(u) }, icon('msg'), 'Message'),
    el('button', { class: 'btn', onclick: () => pokeUser(u) }, icon('bell'), 'Poke'),
    el('button', { class: 'btn', onclick: () => act('peer_mute', { id: u.id, on: !u.localMute }) }, icon(u.localMute ? 'vol' : 'volX'), u.localMute ? 'Unmute' : 'Mute for me'),
    el('button', { class: 'btn', onclick: () => openVerify(u) }, icon('shieldCheck'), u.verified ? 'Verification…' : 'Verify…')));
  if (S.whisperAllowed) {
    const wb = el('button', { class: 'btn whisper-btn', title: 'Hold to talk only to ' + u.nick }, icon('whisper'), 'Hold to whisper');
    holdButton(wb, async () => { await api('quick_whisper', { ids: [u.id] }); AE.whisperUI = 255; }, () => { AE.whisperUI = 0; });
    add(box, el('div', { class: 'actions' }, wb, el('button', { class: 'btn', onclick: () => addToWhisperList({ user: u }) }, icon('plus'), 'Add to whisper list…')));
  }
  if (u.owner && !S.owner) add(box, el('div', { class: 'hint', text: 'This is the server owner. Nobody can kick, ban, mute, move or change the roles of the owner.' }));
  if (canAct(u)) add(box, el('div', { class: 'actions' },
    can('roles.assign') && el('button', { class: 'btn', onclick: () => openAssignRoles(u) }, icon('shield'), 'Roles…'),
    can('client.move') && u.channel !== S.myChannel && el('button', { class: 'btn', onclick: () => act('move', { id: u.id, channel: S.myChannel }) }, icon('move'), 'Move to my channel'),
    can('client.mute') && el('button', { class: 'btn', onclick: () => act('server_mute', { id: u.id, on: !u.serverMuted }) }, icon('micOff'), u.serverMuted ? 'Server-unmute' : 'Server-mute'),
    can('client.kick.channel') && el('button', { class: 'btn danger', onclick: () => kickUser(u, false) }, icon('kick'), 'Kick from channel'),
    can('client.kick.server') && el('button', { class: 'btn danger', onclick: () => kickUser(u, true) }, icon('kick'), 'Kick from server'),
    can('client.ban') && el('button', { class: 'btn danger', onclick: () => banUser(u) }, icon('ban'), 'Ban')));
}

function renderStatus() {
  const conn = $('st-conn');
  if (!S) return;
  if (S.connected) conn.replaceChildren(el('span', { class: 'dot on' }), 'Connected to ' + S.addr);
  else if (S.connecting) conn.replaceChildren(el('span', { class: 'dot busy' }), 'Connecting…');
  else conn.replaceChildren(el('span', { class: 'dot' }), 'Not connected');
  $('st-transport').textContent = S.connected && stats.transport ? 'Voice: ' + stats.transport : '';
  $('st-ping').textContent = S.connected && stats.ping != null ? 'Ping ' + stats.ping + ' ms' : '';
  $('st-e2e').replaceChildren(icon('lock'), 'End-to-end encrypted · AES-256-GCM');
  $('st-e2e').classList.toggle('hidden', !S.connected);
  $('st-id').textContent = 'ID ' + S.identity.slice(0, 14) + '…';
  const audioWarn = AE.micError || (AE.suspended ? 'Click anywhere in the window to enable audio' : '');
  $('st-audio').textContent = audioWarn ? '⚠ ' + audioWarn : (AE.codec ? AE.codecName() : '');
  $('st-audio').classList.toggle('warn', !!audioWarn);
}

// ---------------------------------------------------------------- chat
function tabKeyFor(d) {
  if (d.kind === 'system') return 'server';
  if (d.kind === 'channel') return 'channel';
  if (d.kind === 'server') return 'server';
  if (d.kind === 'private') return 'pm:' + (d.self ? d.target : d.from);
  return 'server';
}

function addChat(d) {
  const key = tabKeyFor(d);
  let title;
  if (key.startsWith('pm:')) {
    const u = userById(+key.slice(3));
    title = u ? u.nick : (d.fromNick || 'Private');
  }
  const tab = ensureTab(key, title, key.startsWith('pm:'));
  tab.msgs.push(d);
  if (tab.msgs.length > 600) tab.msgs.splice(0, tab.msgs.length - 600);
  if (key !== activeTab && d.kind !== 'system') tab.unread++;
  if (key === activeTab) appendMessage(d);
  renderTabs();
}

function addDivider(key, text) {
  const tab = ensureTab(key);
  const d = { kind: 'divider', text, ts: Date.now() };
  tab.msgs.push(d);
  if (key === activeTab) appendMessage(d);
}

function fmtTime(ts) {
  const d = new Date(ts || Date.now());
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

function linkify(text) {
  const out = [];
  const re = /\bhttps?:\/\/[^\s<>"']+/g;
  let last = 0, m;
  while ((m = re.exec(text))) {
    if (m.index > last) out.push(document.createTextNode(text.slice(last, m.index)));
    const url = m[0];
    out.push(el('a', { href: '#', text: url, title: 'Open in your browser', onclick: (e) => { e.preventDefault(); act('open_url', { url }); } }));
    last = m.index + m[0].length;
  }
  if (last < text.length) out.push(document.createTextNode(text.slice(last)));
  return out;
}

function messageNode(d) {
  if (d.kind === 'divider') return el('div', { class: 'msg divider', text: '— ' + d.text + ' · ' + fmtTime(d.ts) + ' —' });
  if (d.kind === 'system') return el('div', { class: 'msg system' }, el('span', { class: 'time', text: fmtTime(d.ts) }), d.text);
  const who = el('span', { class: 'who', text: d.fromNick + (d.verified ? ' ✓' : '') + ':' });
  who.onclick = () => { const u = userById(d.from); if (u) select({ type: 'client', id: u.id }); };
  const n = el('div', { class: 'msg' + (d.self ? ' self' : '') }, el('span', { class: 'time', text: fmtTime(d.ts) }), who, ...linkify(d.text || ''));
  if (d.file) n.append(fileCard(d.file, d));
  if (d.self && d.kind === 'channel' && d.recipients === 0) n.append(el('span', { class: 'time', text: ' (nobody else here)' }));
  return n;
}

function appendMessage(d) {
  const box = $('messages');
  const atBottom = box.scrollHeight - box.scrollTop - box.clientHeight < 40;
  add(box, messageNode(d));
  if (atBottom) box.scrollTop = box.scrollHeight;
}

function renderMessages() {
  const box = $('messages');
  const tab = tabs.get(activeTab);
  box.replaceChildren(...(tab ? tab.msgs.map(messageNode) : []));
  box.scrollTop = box.scrollHeight;
  const input = $('chat-input');
  $('btn-attach').disabled = !(S && S.connected) || activeTab === 'server';
  if (activeTab === 'server') input.placeholder = 'Message everyone on the server (encrypted to each user)…';
  else if (activeTab === 'channel') input.placeholder = 'Message your channel…';
  else input.placeholder = 'Private message to ' + (tab ? tab.title : '') + '…';
}

function renderTabs() {
  const bar = $('tabs');
  bar.replaceChildren();
  if (S && S.connected) {
    const ch = chanById(S.myChannel);
    if (ch) tabs.get('channel').title = ch.name;
  }
  for (const [key, tab] of tabs) {
    if (key === 'channel' && !(S && S.connected)) continue;
    const t = el('div', { class: 'tab' + (key === activeTab ? ' active' : '') },
      icon(key === 'server' ? 'globe' : key === 'channel' ? 'hash' : 'msg'),
      el('span', { text: tab.title }),
      tab.unread > 0 && el('span', { class: 'unread', text: tab.unread }));
    t.querySelector('svg').style.cssText = 'width:13px;height:13px';
    if (tab.closable) {
      const x = el('span', { class: 'x', title: 'Close' }, icon('x'));
      x.onclick = (e) => { e.stopPropagation(); tabs.delete(key); if (activeTab === key) switchTab('channel'); else renderTabs(); };
      t.append(x);
    }
    t.onclick = () => switchTab(key);
    bar.append(t);
  }
}

function switchTab(key) {
  if (!tabs.has(key)) key = 'server';
  activeTab = key;
  tabs.get(key).unread = 0;
  renderTabs();
  renderMessages();
  $('chat-input').focus();
}

function openPM(u) {
  ensureTab('pm:' + u.id, u.nick, true);
  switchTab('pm:' + u.id);
}

$('chat-form').onsubmit = async (e) => {
  e.preventDefault();
  const input = $('chat-input');
  const text = input.value.trim();
  if (!text || !S.connected) return;
  let args;
  if (activeTab === 'server') args = { kind: 'server', text };
  else if (activeTab === 'channel') args = { kind: 'channel', text };
  else args = { kind: 'private', target: +activeTab.slice(3), text };
  input.value = '';
  const r = await api('chat', args).then(() => true, (err) => { toast(err.message, 'error'); return false; });
  if (!r) input.value = text;
};

// ---------------------------------------------------------------- actions
async function joinChannel(ch) {
  if (!S.connected || ch.id === S.myChannel) return;
  let password = '';
  if (ch.pw && !can('channel.join.any')) {
    password = await promptModal({ title: 'Join ' + ch.name, icon: 'lock', label: 'Channel password', type: 'password', ok: 'Join' });
    if (password == null) return;
  }
  act('join', { id: ch.id, password });
}

async function deleteChannel(ch) {
  const ok = await confirmModal('Delete channel', `Delete "${ch.name}" and all its sub-channels? Users inside are moved to the default channel.`, 'Delete', true);
  if (ok) act('chan_delete', { id: ch.id });
}

async function pokeUser(u) {
  const text = await promptModal({ title: 'Poke ' + u.nick, icon: 'bell', label: 'Message (end-to-end encrypted)', ok: 'Poke' });
  if (text != null) act('poke', { target: u.id, text: text || 'Poke!' });
}

async function kickUser(u, server) {
  const reason = await promptModal({ title: (server ? 'Kick from server: ' : 'Kick from channel: ') + u.nick, icon: 'kick', label: 'Reason (optional)', ok: 'Kick', danger: true });
  if (reason != null) act('kick', { id: u.id, server, reason });
}

function banUser(u) {
  const dur = el('select', {}, ...[['10 minutes', 10], ['1 hour', 60], ['1 day', 1440], ['1 week', 10080], ['Permanent', 0]].map(([l, v]) => el('option', { value: v, text: l })));
  dur.value = '60';
  const reason = el('input', { placeholder: 'Reason (optional)' });
  modal({
    title: 'Ban ' + u.nick, icon: 'ban',
    body: [el('div', { class: 'callout warn', text: 'Bans apply to the user\'s IP address. Everyone connected from that address is disconnected.' }),
      el('div', { class: 'field' }, el('label', { text: 'Duration' }), dur),
      el('div', { class: 'field' }, el('label', { text: 'Reason' }), reason)],
    buttons: [{ label: 'Cancel' }, { label: 'Ban', danger: true, fn: () => act('ban', { id: u.id, minutes: +dur.value, reason: reason.value }) }],
  });
}

async function changeNick() {
  const nick = await promptModal({ title: 'Change nickname', icon: 'user', label: 'Nickname', value: S.nickname, ok: 'Save' });
  if (nick) act('nick', { nick });
}

async function toggleAway() {
  if (S.away) return act('status', { away: false, awayMsg: '' });
  const msg = await promptModal({ title: 'Set away', icon: 'moon', label: 'Away message (optional)', ok: 'Set away' });
  if (msg != null) act('status', { away: true, awayMsg: msg });
}

async function adminButton(e) {
  e.stopPropagation(); // the document click handler would close the menu right away
  if (!S.connected) return;
  const staff = S.owner || staffPerms.some(can);
  if (!staff) return useToken();
  const r = e.currentTarget.getBoundingClientRect();
  showCtx(r.left, r.bottom + 4, [
    (can('roles.manage') || can('roles.assign') || can('tokens.create')) && { label: 'Roles & permissions…', icon: 'shield', fn: () => openRoleManager() },
    can('tokens.create') && { label: 'Privilege keys…', icon: 'key', fn: () => openRoleManager('keys') },
    can('ban.list') && { label: 'Ban list', icon: 'list', fn: openBans },
    can('server.edit') && { label: 'Edit server name & welcome', icon: 'edit', fn: openServerEdit },
    can('server.password') && { label: 'Server password…', icon: 'lock', fn: openServerPassword },
    'sep',
    { label: 'Use privilege key…', icon: 'key', fn: useToken },
  ]);
}

async function useToken() {
  const t = await promptModal({ title: 'Use privilege key', icon: 'key', label: 'Privilege key (e.g. SS-ABCD-EFGH-…)', ok: 'Redeem' });
  if (t && t.trim()) act('token_use', { token: t.trim() });
}

// ---------------------------------------------------------------- context menus
function showCtx(x, y, items) {
  const m = $('ctx');
  m.replaceChildren();
  for (const it of items.filter(Boolean)) {
    if (it === 'sep') { m.append(el('div', { class: 'sep' })); continue; }
    const n = el('div', { class: 'it' + (it.danger ? ' danger' : '') }, icon(it.icon || 'move'), el('span', { text: it.label }));
    n.onclick = () => { hideCtx(); it.fn(); };
    m.append(n);
  }
  m.classList.remove('hidden');
  const w = m.offsetWidth, h = m.offsetHeight;
  m.style.left = Math.min(x, innerWidth - w - 8) + 'px';
  m.style.top = Math.min(y, innerHeight - h - 8) + 'px';
}
function hideCtx() { $('ctx').classList.add('hidden'); }
document.addEventListener('click', (e) => { if (!$('ctx').contains(e.target)) hideCtx(); });
document.addEventListener('contextmenu', (e) => { if (!e.target.closest('.row')) e.preventDefault(); });

function channelMenu(e, ch) {
  showCtx(e.clientX, e.clientY, [
    ch.id !== S.myChannel && { label: 'Join channel', icon: 'enter', fn: () => joinChannel(ch) },
    { label: 'Create sub-channel', icon: 'folderPlus', fn: () => openChannelModal(null, ch.id) },
    canEdit(ch) && { label: 'Edit channel', icon: 'edit', fn: () => openChannelModal(ch) },
    canDelete(ch) && !ch.def && { label: 'Delete channel', icon: 'trash', danger: true, fn: () => deleteChannel(ch) },
    S.whisperAllowed && { label: 'Add to whisper list…', icon: 'whisper', fn: () => addToWhisperList({ channel: ch }) },
    'sep',
    { label: 'Create channel', icon: 'plus', fn: () => openChannelModal(null, 0) },
  ]);
}

function serverMenu(e) {
  showCtx(e.clientX, e.clientY, [
    { label: 'Create channel', icon: 'folderPlus', fn: () => openChannelModal(null, 0) },
    can('server.edit') && { label: 'Edit server', icon: 'edit', fn: openServerEdit },
    (can('roles.manage') || can('roles.assign') || can('tokens.create')) && { label: 'Roles & permissions…', icon: 'shield', fn: () => openRoleManager() },
    can('ban.list') && { label: 'Ban list', icon: 'list', fn: openBans },
    'sep',
    { label: 'Disconnect', icon: 'unplug', fn: () => act('disconnect') },
  ]);
}

function userMenu(e, u) {
  if (u.me) {
    return showCtx(e.clientX, e.clientY, [
      { label: 'Change nickname', icon: 'edit', fn: changeNick },
      { label: S.away ? 'Set back' : 'Set away', icon: 'moon', fn: toggleAway },
      { label: S.muted ? 'Unmute microphone' : 'Mute microphone', icon: 'micOff', fn: () => act('status', { muted: !S.muted }) },
      { label: 'Change picture…', icon: 'image', fn: chooseAvatar },
      { label: 'Settings', icon: 'sliders', fn: openSettings },
    ]);
  }
  showCtx(e.clientX, e.clientY, [
    u.sharing && u.channel === S.myChannel && { label: 'Watch screen', icon: 'monitor', fn: () => watchUser(u) },
    { label: 'Send private message', icon: 'msg', fn: () => openPM(u) },
    { label: 'Poke', icon: 'bell', fn: () => pokeUser(u) },
    { label: u.localMute ? 'Unmute for me' : 'Mute for me', icon: u.localMute ? 'vol' : 'volX', fn: () => act('peer_mute', { id: u.id, on: !u.localMute }) },
    { label: 'Verify identity…', icon: 'shieldCheck', fn: () => openVerify(u) },
    S.whisperAllowed && { label: 'Add to whisper list…', icon: 'whisper', fn: () => addToWhisperList({ user: u }) },
    canAct(u) && 'sep',
    canAct(u) && can('roles.assign') && { label: 'Roles…', icon: 'shield', fn: () => openAssignRoles(u) },
    canAct(u) && can('client.move') && u.channel !== S.myChannel && { label: 'Move to my channel', icon: 'move', fn: () => act('move', { id: u.id, channel: S.myChannel }) },
    canAct(u) && can('client.mute') && { label: u.serverMuted ? 'Server-unmute' : 'Server-mute', icon: 'micOff', fn: () => act('server_mute', { id: u.id, on: !u.serverMuted }) },
    canAct(u) && can('client.kick.channel') && { label: 'Kick from channel', icon: 'kick', danger: true, fn: () => kickUser(u, false) },
    canAct(u) && can('client.kick.server') && { label: 'Kick from server', icon: 'kick', danger: true, fn: () => kickUser(u, true) },
    canAct(u) && can('client.ban') && { label: 'Ban…', icon: 'ban', danger: true, fn: () => banUser(u) },
  ]);
}

// ---------------------------------------------------------------- modals
let modalStack = [];
function modal({ title, icon: ic, body, buttons, wide, onClose }) {
  const root = $('modal-root');
  const foot = el('div', { class: 'foot' });
  const box = el('div', { class: 'modal' + (wide ? ' wide' : '') }, el('h2', {}, icon(ic || 'shield'), title), el('div', { class: 'body' }, body), foot);
  const ov = el('div', { class: 'overlay' }, box);
  const close = () => {
    ov.remove();
    modalStack = modalStack.filter((m) => m !== handle);
    if (onClose) onClose();
  };
  const handle = { close, ov, primary: null };
  for (const b of buttons || [{ label: 'Close' }]) {
    const n = el('button', { class: 'btn' + (b.primary ? ' primary' : '') + (b.danger ? ' danger solid' : ''), type: 'button', text: b.label });
    n.onclick = async () => {
      if (b.fn) {
        n.disabled = true;
        const r = await b.fn();
        n.disabled = false;
        if (r === false) return;
      }
      close();
    };
    if (b.primary || b.danger) handle.primary = n;
    foot.append(n);
  }
  ov.addEventListener('mousedown', (e) => { if (e.target === ov) close(); });
  box.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && e.target.tagName !== 'TEXTAREA' && e.target.tagName !== 'BUTTON' && handle.primary) { e.preventDefault(); handle.primary.click(); }
  });
  root.append(ov);
  modalStack.push(handle);
  const first = box.querySelector('input:not([type=checkbox]):not([type=range]),textarea');
  if (first) setTimeout(() => first.focus(), 30);
  return handle;
}

function promptModal({ title, icon: ic, label, value = '', type = 'text', ok = 'OK', danger }) {
  return new Promise((resolve) => {
    let done = false;
    const input = el('input', { type, value });
    modal({
      title, icon: ic,
      body: el('div', { class: 'field' }, el('label', { text: label }), input),
      buttons: [{ label: 'Cancel' }, { label: ok, primary: !danger, danger, fn: () => { done = true; resolve(input.value); } }],
      onClose: () => { if (!done) resolve(null); },
    });
  });
}

function confirmModal(title, text, ok, danger) {
  return new Promise((resolve) => {
    let done = false;
    modal({
      title, icon: danger ? 'alert' : 'shield', body: el('div', { text }),
      buttons: [{ label: 'Cancel' }, { label: ok, primary: !danger, danger, fn: () => { done = true; resolve(true); } }],
      onClose: () => { if (!done) resolve(false); },
    });
  });
}

function toast(text, type) {
  const t = el('div', { class: 'toast ' + (type || ''), text });
  $('toasts').append(t);
  setTimeout(() => t.remove(), 5500);
}

// ---- connect
function parseAddress(s) {
  s = s.trim();
  let host = s, port = 9987;
  const v6 = s.match(/^\[(.+)\](?::(\d+))?$/);
  if (v6) { host = v6[1]; if (v6[2]) port = +v6[2]; }
  else if ((s.match(/:/g) || []).length === 1) { const [h, p] = s.split(':'); host = h; port = +p || 9987; }
  return { host, port };
}

function openConnect() {
  if (!S || S.connected) return;
  if (modalStack.some((m) => m.kind === 'connect')) return;
  const addr = el('input', { placeholder: 'voice.example.com or 203.0.113.5:9987', value: S.lastHost ? (S.lastHost.includes(':') ? `[${S.lastHost}]` : S.lastHost) + (S.lastPort && S.lastPort !== 9987 ? ':' + S.lastPort : '') : '' });
  const nick = el('input', { maxlength: 32, value: S.nickname || '' });
  const pw = el('input', { type: 'password', placeholder: 'only if the server requires one' });
  const save = el('input', { type: 'checkbox' });
  const bmName = el('input', { placeholder: 'Bookmark name' });
  const list = el('div', { class: 'bm-list' }, el('div', { class: 'head', text: 'Bookmarks' }));
  const fill = (b) => { addr.value = b.host + (b.port && b.port !== 9987 ? ':' + b.port : ''); if (b.nickname) nick.value = b.nickname; pw.value = b.serverPassword || ''; bmName.value = b.name; };
  if (!S.bookmarks.length) list.append(el('div', { style: 'color:var(--faint);font-size:12px', text: 'No bookmarks yet.' }));
  for (const b of S.bookmarks) {
    const del = el('span', { class: 'icon-btn del', title: 'Delete' }, icon('trash'));
    del.onclick = (e) => { e.stopPropagation(); act('bookmark_delete', { name: b.name }).then(() => { h.close(); setTimeout(openConnect, 50); }); };
    const row = el('div', { class: 'bm' }, el('span', { class: 'n' }, b.name, el('span', { class: 'h', text: b.host + ':' + b.port })), del);
    row.onclick = () => fill(b);
    row.ondblclick = () => { fill(b); h.primary.click(); };
    list.append(row);
  }
  const form = el('div', {},
    el('div', { class: 'field' }, el('label', { text: 'Server address' }), addr),
    el('div', { class: 'field' }, el('label', { text: 'Nickname' }), nick),
    el('div', { class: 'field' }, el('label', { text: 'Server password' }), pw),
    el('label', { class: 'check' }, save, 'Save as bookmark'),
    el('div', { class: 'field' }, bmName),
    el('div', { class: 'callout', style: 'margin-bottom:0' }, 'No account needed. Others recognise you by your identity fingerprint:', el('div', { class: 'fp', style: 'margin-top:6px', text: S.identity })));
  const h = modal({
    title: 'Connect to server', icon: 'plug', wide: true,
    body: el('div', { class: 'connect-layout' }, list, form),
    buttons: [{ label: 'Cancel' }, {
      label: 'Connect', primary: true, fn: async () => {
        const { host, port } = parseAddress(addr.value);
        if (!host) { toast('Enter a server address', 'error'); return false; }
        if (!nick.value.trim()) { toast('Enter a nickname', 'error'); return false; }
        if (save.checked) await act('bookmark_save', { name: bmName.value || host, host, port, nickname: nick.value, serverPassword: pw.value });
        doConnect(host, port, nick.value, pw.value, '');
      },
    }],
  });
  h.kind = 'connect';
}

async function doConnect(host, port, nick, password, trust) {
  let r;
  try { r = await api('connect', { host, port, nick, password, trust }); }
  catch (e) {
    toast(e.message, 'error');
    if (/password/i.test(e.message)) {
      const pw = await promptModal({ title: 'Server password', icon: 'lock', label: 'This server requires a password', type: 'password', ok: 'Connect' });
      if (pw != null) doConnect(host, port, nick, pw, trust);
    }
    return;
  }
  if (r && r.untrusted) {
    const changed = !!r.known;
    modal({
      title: changed ? 'Server certificate changed!' : 'New server', icon: changed ? 'alert' : 'shield',
      body: [
        changed
          ? el('div', { class: 'callout danger', text: 'The certificate of ' + r.addr + ' is different from the one you trusted before. The server may have been reinstalled — or someone may be intercepting your connection. Only continue if the server owner confirms the new fingerprint.' })
          : el('div', { class: 'callout', text: 'First connection to ' + r.addr + '. Compare this fingerprint with the one the server prints at startup (securespeak-server -fingerprint). It will be remembered.' }),
        changed && el('div', { class: 'field' }, el('label', { text: 'Previously trusted' }), el('div', { class: 'fp', style: 'font-size:11px', text: r.known })),
        el('div', { class: 'field' }, el('label', { text: 'Server certificate (SHA-256)' }), el('div', { class: 'fp', style: 'font-size:11px', text: r.fp })),
        el('div', { style: 'color:var(--faint);font-size:12px', text: 'Note: voice and chat stay end-to-end encrypted either way; this check protects your server password and metadata.' }),
      ],
      buttons: [{ label: 'Cancel' }, { label: changed ? 'Trust new certificate' : 'Trust and connect', primary: !changed, danger: changed, fn: () => { doConnect(host, port, nick, password, r.fp); } }],
    });
  }
}

// ---- channel create/edit
function openChannelModal(ch, parent) {
  if (!S.connected) return;
  const editing = !!ch;
  const name = el('input', { maxlength: 40, value: ch ? ch.name : '' });
  const topic = el('input', { maxlength: 120, value: ch ? ch.topic || '' : '' });
  const desc = el('textarea', { maxlength: 2000 });
  desc.value = ch ? ch.desc || '' : '';
  const pw = el('input', { type: 'password', placeholder: editing && ch.pw ? 'leave empty to keep current password' : 'none' });
  const rmPw = el('input', { type: 'checkbox' });
  const max = el('input', { type: 'number', min: 0, value: ch ? ch.max || 0 : 0 });
  const permAllowed = editing ? can('channel.edit') : can('channel.create.permanent');
  const perm = el('input', { type: 'checkbox', checked: ch ? !!ch.perm : (permAllowed && !can('channel.create.temporary')) });
  const def = el('input', { type: 'checkbox', checked: ch ? !!ch.def : false });
  const order = el('input', { type: 'number', value: ch ? ch.order || 0 : 0 });
  const parentName = parent ? (chanById(parent) || {}).name : null;
  modal({
    title: editing ? 'Edit channel' : (parentName ? 'New sub-channel in ' + parentName : 'New channel'), icon: editing ? 'edit' : 'folderPlus',
    body: [
      el('div', { class: 'grid2' },
        el('div', { class: 'field' }, el('label', { text: 'Name' }), name),
        el('div', { class: 'field' }, el('label', { text: 'Max users (0 = ∞)' }), max)),
      el('div', { class: 'field' }, el('label', { text: 'Topic' }), topic),
      el('div', { class: 'field' }, el('label', { text: 'Description' }), desc),
      el('div', { class: 'field' }, el('label', { text: 'Password' }), pw, editing && ch.pw && el('label', { class: 'check' }, rmPw, 'Remove password')),
      permAllowed
        ? el('div', {}, el('label', { class: 'check' }, perm, 'Permanent (survives restarts and when empty)'),
          editing && el('label', { class: 'check' }, def, 'Default channel for new users'),
          editing && el('div', { class: 'field', style: 'max-width:140px' }, el('label', { text: 'Sort order' }), order))
        : el('div', { class: 'callout', text: 'Temporary channel: it is deleted automatically when the last person leaves. You will join it right away.' }),
    ],
    buttons: [{ label: 'Cancel' }, {
      label: editing ? 'Save' : 'Create', primary: true, fn: async () => {
        if (!name.value.trim()) { toast('Name required', 'error'); return false; }
        if (editing) {
          const a = { id: ch.id, name: name.value, topic: topic.value, desc: desc.value, max: +max.value || 0, perm: permAllowed ? perm.checked : ch.perm, def: def.checked, order: +order.value || 0 };
          if (rmPw.checked) a.pw = ''; else if (pw.value) a.pw = pw.value;
          await act('chan_edit', a);
        } else {
          await act('chan_create', { parent: parent || 0, name: name.value, topic: topic.value, desc: desc.value, pw: pw.value, max: +max.value || 0, perm: permAllowed && perm.checked });
        }
      },
    }],
  });
}

// ---- server admin
function openServerEdit() {
  const name = el('input', { maxlength: 60, value: S.serverName });
  const welcome = el('textarea', { maxlength: 2000 });
  welcome.value = S.welcomeMsg || '';
  modal({
    title: 'Server settings', icon: 'server',
    body: [el('div', { class: 'field' }, el('label', { text: 'Server name' }), name), el('div', { class: 'field' }, el('label', { text: 'Welcome message' }), welcome)],
    buttons: [{ label: 'Cancel' }, { label: 'Save', primary: true, fn: () => act('server_edit', { name: name.value, welcome: welcome.value }) }],
  });
}

function openBans() {
  const box = el('div', {}, el('div', { style: 'color:var(--faint)', text: 'Loading…' }));
  bansView = (bans) => {
    if (!bans.length) return box.replaceChildren(el('div', { style: 'color:var(--faint)', text: 'No active bans.' }));
    const tbl = el('table', { class: 'list' }, el('tr', {}, ...['Nickname', 'IP', 'Reason', 'Until', 'By', ''].map((h) => el('th', { text: h }))));
    for (const b of bans) {
      const un = el('button', { class: 'btn', text: 'Unban' });
      un.onclick = () => act('unban', { ip: b.ip });
      tbl.append(el('tr', {}, el('td', { text: b.nick }), el('td', { class: 'mono', text: b.ip }), el('td', { text: b.reason || '' }),
        el('td', { text: b.until ? new Date(b.until * 1000).toLocaleString() : 'Permanent' }), el('td', { text: b.by || '' }), el('td', {}, un)));
    }
    box.replaceChildren(tbl);
  };
  modal({ title: 'Ban list', icon: 'ban', wide: true, body: box, onClose: () => { bansView = null; } });
  act('ban_list');
}

// ---- verification
function openVerify(u) {
  modal({
    title: 'Verify ' + u.nick, icon: 'shieldCheck',
    body: [
      el('div', { class: 'callout', text: 'Compare both fingerprints with ' + u.nick + ' over a channel the server cannot tamper with: in person, a phone call, or another messenger. If they match exactly, nobody can intercept your conversation — not even the server operator.' }),
      el('div', { class: 'field' }, el('label', { text: u.nick + '\'s fingerprint' }), el('div', { class: 'fp', text: u.fp })),
      el('div', { class: 'field' }, el('label', { text: 'Your fingerprint' }), el('div', { class: 'fp', text: S.identity })),
    ],
    buttons: [{ label: 'Close' }, u.verified
      ? { label: 'Remove verification', danger: true, fn: () => act('verify', { id: u.id, on: false }) }
      : { label: 'Fingerprints match — mark verified', primary: true, fn: () => act('verify', { id: u.id, on: true }) }],
  });
}

// ---- poke
function showPoke(d) {
  modal({
    title: d.fromNick + ' poked you', icon: 'bell',
    body: el('div', { style: 'font-size:15px;white-space:pre-wrap', text: d.text }),
    buttons: [{ label: 'Close' }, { label: 'Reply', primary: true, fn: () => { const u = userById(d.from); if (u) openPM(u); } }],
  });
}

// ---- settings
let mediaDevices = { inputs: [], outputs: [] };
async function refreshDevices() {
  try {
    const list = await navigator.mediaDevices.enumerateDevices();
    const name = (d, i, kind) => d.label || (kind + ' ' + (i + 1));
    mediaDevices.inputs = [{ id: '', name: 'System default' }, ...list.filter((d) => d.kind === 'audioinput' && d.deviceId !== 'default' && d.deviceId !== 'communications').map((d, i) => ({ id: d.deviceId, name: name(d, i, 'Microphone') }))];
    mediaDevices.outputs = [{ id: '', name: 'System default' }, ...list.filter((d) => d.kind === 'audiooutput' && d.deviceId !== 'default' && d.deviceId !== 'communications').map((d, i) => ({ id: d.deviceId, name: name(d, i, 'Speakers') }))];
  } catch (e) { /* not available */ }
}

async function captureKey(keycap) {
  keycap.textContent = 'Press a key…';
  keycap.classList.add('wait');
  const r = await act('capture_key', { for: keycap.dataset.for || '' });
  keycap.classList.remove('wait');
  return r;
}

async function openSettings(initialTab) {
  AE.setMicWanted(true);
  await refreshDevices();
  const view = el('div');
  const tabsBar = el('div', { class: 'stabs' });
  let tab = typeof initialTab === 'string' ? initialTab : 'audio';
  const set = (patch) => act('settings', patch);
  let lvlRefs = null;

  const render = () => {
    tabsBar.replaceChildren(...[['audio', 'Audio & voice'], ['whisper', 'Whisper'], ['general', 'Profile & security']].map(([k, l]) => {
      const b = el('button', { class: k === tab ? 'on' : '', text: l });
      b.onclick = () => { tab = k; render(); };
      return b;
    }));
    view.replaceChildren();
    lvlRefs = null;
    if (tab === 'audio') renderAudioTab();
    else if (tab === 'whisper') renderWhisperTab(view);
    else renderGeneralTab();
  };

  const range = (min, max, step, value, fmt, key, scale = 1) => {
    const v = el('span', { class: 'val', text: fmt(value) });
    const r = el('input', { type: 'range', min, max, step, value: value * scale });
    r.oninput = () => { v.textContent = fmt(r.value / scale); set({ [key]: r.value / scale }); };
    r.onchange = () => r.blur();
    return [r, v];
  };
  const check = (label, key, hint) => el('label', { class: 'check', title: hint || '' },
    el('input', { type: 'checkbox', checked: !!S.audio[key], onchange: (e) => set({ [key]: e.target.checked }) }), label);

  function renderAudioTab() {
    const a = S.audio;
    const sel = (list, value, key) => {
      const s = el('select', {}, ...list.map((d) => el('option', { value: d.id, text: d.name })));
      s.value = list.some((d) => d.id === value) ? value : '';
      s.onchange = () => set({ [key]: s.value });
      return s;
    };
    const mode = el('div', { class: 'seg' }, ...[['va', 'Voice activation'], ['ptt', 'Push-to-talk'], ['continuous', 'Always on']].map(([k, l]) => {
      const b = el('button', { class: a.mode === k ? 'on' : '', text: l, type: 'button' });
      b.onclick = () => set({ mode: k });
      return b;
    }));
    const lvl = el('div', { class: 'lvl' }), th = el('div', { class: 'th' });
    th.style.left = dbToPct(a.vaThreshold) + '%';
    lvlRefs = { lvl };
    const keycap = el('span', { class: 'keycap', text: S.pttKeyName, 'data-for': 'ptt' });
    const sup = navigator.mediaDevices && navigator.mediaDevices.getSupportedConstraints ? navigator.mediaDevices.getSupportedConstraints() : {};
    const info = AE.micInfo();
    const loop = el('input', { type: 'checkbox', checked: AE.loopback, onchange: (e) => AE.setLoopback(e.target.checked) });
    add(view,
      AE.micError && el('div', { class: 'callout danger', text: AE.micError }),
      AE.suspended && el('div', { class: 'callout warn', text: 'Audio is paused by the browser. Click anywhere in this window to enable it.' }),
      el('div', { class: 'grid2', style: 'grid-template-columns:1fr 1fr' },
        el('div', { class: 'field' }, el('label', { text: 'Microphone' }), sel(mediaDevices.inputs, a.inputDevice, 'inputDevice')),
        el('div', { class: 'field' }, el('label', { text: 'Speakers / headset' + (AE.ctx && AE.ctx.setSinkId ? '' : ' (system default only in this browser)') }), sel(mediaDevices.outputs, a.outputDevice, 'outputDevice'))),
      el('div', { class: 'field' }, el('label', { text: 'Transmission' }), mode),
      el('div', { class: 'field' }, el('label', { text: 'Microphone level' + (a.mode === 'va' ? ' — speak normally and put the yellow marker just above the background noise' : '') }),
        el('div', { class: 'meter-big' }, lvl, a.mode === 'va' && th)),
      a.mode === 'va' && el('div', { class: 'slider-row' }, el('label', { text: 'Threshold' }), ...range(-70, -5, 1, a.vaThreshold, (x) => Math.round(x) + ' dB', 'vaThreshold')),
      a.mode === 'ptt' && el('div', { class: 'field', style: 'margin-top:10px' },
        el('label', { text: 'Push-to-talk key' }),
        el('div', { style: 'display:flex;gap:8px;align-items:center;flex-wrap:wrap' },
          keycap,
          el('button', { class: 'btn', type: 'button', disabled: !S.hotkeySupported, onclick: () => captureKey(keycap) }, icon('key'), 'Set key'),
          S.audio.pttKey > 0 && el('button', { class: 'btn', type: 'button', onclick: () => act('clear_ptt_key') }, 'Clear')),
        el('div', { class: 'hint', text: S.hotkeyInfo + ' The toolbar button always works.' })),
      a.mode === 'ptt' && el('div', { class: 'slider-row' }, el('label', { text: 'Release delay' }), ...range(0, 1000, 10, a.pttReleaseMs, (x) => Math.round(x) + ' ms', 'pttReleaseMs')),
      el('div', { class: 'section-title', text: 'Voice processing' }),
      el('div', { class: 'grid2', style: 'grid-template-columns:1fr 1fr;gap:0 16px' },
        check('Noise suppression', 'noiseSuppression', 'Removes fans, keyboard and background noise'),
        check('Echo cancellation', 'echoCancellation', 'Needed when using speakers instead of a headset'),
        check('Automatic gain control', 'autoGain', 'Keeps your volume steady'),
        sup.voiceIsolation && check('Voice isolation (strong)', 'voiceIsolation', 'Aggressive AI noise removal where supported')),
      el('div', { class: 'slider-row' }, el('label', { text: 'Opus bitrate' }), ...range(16, 128, 8, a.bitrate, (x) => Math.round(x) + ' kbit/s', 'bitrate')),
      el('div', { class: 'slider-row' }, el('label', { text: 'Mic boost' }), ...range(0.25, 4, 0.05, a.inputGain, (x) => (+x).toFixed(2) + '×', 'inputGain')),
      el('div', { class: 'slider-row' }, el('label', { text: 'Output volume' }), ...range(0, 200, 5, a.outputVolume, (x) => Math.round(x * 100) + '%', 'outputVolume', 100)),
      el('div', { style: 'display:flex;gap:16px;align-items:center;margin-top:12px;flex-wrap:wrap' },
        el('label', { class: 'check', style: 'margin:0' }, loop, 'Hear myself (test codec & processing)'),
        el('button', { class: 'btn', type: 'button', onclick: () => { const on = S.audio.sounds; S.audio.sounds = true; AE.sound('connect'); S.audio.sounds = on; } }, icon('vol'), 'Test speakers')),
      el('label', { class: 'check', style: 'margin-top:12px' }, el('input', { type: 'checkbox', checked: a.sounds, onchange: (e) => set({ sounds: e.target.checked }) }), 'Notification sounds'),
      a.sounds && el('div', { class: 'slider-row' }, el('label', { text: 'Sound volume' }), ...range(0, 150, 5, a.soundVolume, (x) => Math.round(x * 100) + '%', 'soundVolume', 100)),
      el('div', { class: 'hint', style: 'margin-top:10px' }, 'Codec: ' + AE.codecName() + (info ? ' · Mic: ' + (info.label || 'default') + ', ' + (info.settings.sampleRate || '?') + ' Hz, noise suppression ' + (info.settings.noiseSuppression ? 'on' : 'off') : '')),
    );
  }

  function renderGeneralTab() {
    const me = S.clients.find((c) => c.me) || { avatar: S.avatar, me: true };
    me.avatar = S.avatar;
    add(view,
      el('div', { class: 'profile-row' },
        avatarNode(me, true),
        el('div', {},
          el('div', { style: 'font-weight:600;font-size:15px', text: S.nickname || '(no nickname)' }),
          el('div', { class: 'actions', style: 'margin-top:6px' },
            el('button', { class: 'btn', onclick: chooseAvatar }, icon('image'), 'Choose picture'),
            S.avatar && el('button', { class: 'btn', onclick: () => act('avatar_clear') }, 'Remove'),
            el('button', { class: 'btn', onclick: changeNick }, icon('edit'), 'Nickname')))),
      el('div', { class: 'hint', text: 'Your picture is sent end-to-end encrypted to the people on the server; the server never sees it.' }),
      el('div', { class: 'section-title', text: 'Received files' }),
      el('div', { style: 'display:flex;gap:8px;align-items:center' },
        el('span', { class: 'keycap mono', style: 'text-align:left;flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-weight:400', text: S.downloadDir }),
        el('button', { class: 'btn', onclick: () => act('open_downloads') }, icon('folder'), 'Open'),
        el('button', {
          class: 'btn', onclick: async () => {
            const d = await promptModal({ title: 'Download folder', icon: 'folder', label: 'Absolute path (leave empty for the default)', value: S.downloadDir, ok: 'Save' });
            if (d != null) act('download_dir', { dir: d });
          },
        }, 'Change')),
      el('label', { class: 'check', style: 'margin-top:10px' },
        el('input', { type: 'checkbox', checked: S.autoMedia, onchange: (e) => act('auto_media', { on: e.target.checked }) }),
        'Show image and video previews automatically (images up to 16 MB, videos up to 64 MB)'),
      el('div', { class: 'section-title', text: 'Identity' }),
      el('div', { class: 'fp', text: S.identity }),
      el('div', { class: 'callout', text: 'There are no accounts. Your identity is an Ed25519 key pair stored only on this computer' + (S.keyProtected ? ', encrypted with your Windows login (DPAPI)' : ' (file readable only by your user)') + '. Others can verify you by comparing this fingerprint.' }),
      el('div', { class: 'callout' },
        el('b', { text: 'How encryption works' }), el('br'),
        '• Each connection creates a fresh X25519 key, signed by your identity key.', el('br'),
        '• Voice: Opus frames encrypted with AES-256-GCM using a per-speaker key that only the channel (or whisper targets) receive; replaced as soon as someone leaves.', el('br'),
        '• Every voice packet is signed with Ed25519, so nobody — not other members, not the server — can fake your voice.', el('br'),
        '• Text, pokes, pictures: each message gets its own AES-256 key, wrapped separately for each recipient.', el('br'),
        '• Files: a fresh AES-256 key per transfer and recipient, checked with SHA-256 at the end.', el('br'),
        '• The server sees who is connected and who is talking, but never any content.'),
      el('div', { class: 'actions' }, el('button', {
        class: 'btn danger', disabled: S.connected, title: S.connected ? 'Disconnect first' : '',
        onclick: async () => { if (await confirmModal('New identity', 'Generate a new identity key? People who verified you will see you as unverified.', 'Generate', true)) act('new_identity'); },
      }, icon('key'), 'Generate new identity')),
      el('div', { class: 'hint', style: 'margin-top:14px', text: 'SecureSpeak ' + (S.version || 'dev') + ' · ' + S.platform }));
  }

  settingsView = {
    refresh: () => { if (!document.activeElement || document.activeElement.type !== 'range') render(); },
    level: (l) => { if (lvlRefs) { lvlRefs.lvl.style.width = dbToPct(l.db) + '%'; lvlRefs.lvl.classList.toggle('tx', l.tx); } },
  };
  const onDev = () => refreshDevices().then(() => settingsView && settingsView.refresh());
  navigator.mediaDevices?.addEventListener?.('devicechange', onDev);
  const h = modal({
    title: 'Settings', icon: 'sliders', wide: true, body: [tabsBar, el('div', { style: 'height:12px' }), view],
    onClose: () => {
      settingsView = null;
      AE.setLoopback(false);
      AE.setMicWanted(false);
      navigator.mediaDevices?.removeEventListener?.('devicechange', onDev);
    },
  });
  h.ov.querySelector('.body').style.paddingTop = '0';
  render();
  // device labels become available once the microphone permission is granted
  AE.updateMic().then(() => refreshDevices()).then(() => settingsView && settingsView.refresh());
}

// ---------------------------------------------------------------- toolbar wiring
$('btn-connect').onclick = openConnect;
$('btn-disconnect').onclick = () => act('disconnect');
$('btn-mute').onclick = () => act('status', { muted: !S.muted });
$('btn-deaf').onclick = () => act('status', S.deaf ? { deaf: false, muted: false } : { deaf: true });
$('btn-away').onclick = toggleAway;
$('btn-settings').onclick = openSettings;
$('btn-admin').onclick = adminButton;
$('btn-new-channel').onclick = () => openChannelModal(null, 0);
function holdButton(b, down, up) {
  let held = false;
  b.addEventListener('pointerdown', (e) => { e.preventDefault(); b.setPointerCapture?.(e.pointerId); held = true; b.classList.add('down'); down(); });
  const release = () => { if (!held) return; held = false; b.classList.remove('down'); up(); };
  b.addEventListener('pointerup', release);
  b.addEventListener('pointercancel', release);
  b.addEventListener('lostpointercapture', release);
}
holdButton($('btn-ptt'), () => { AE.pttUI = true; }, () => { AE.pttUI = false; });

document.addEventListener('keydown', (e) => {
  if (e.key === 'Escape') {
    hideCtx();
    const top = modalStack[modalStack.length - 1];
    if (top) top.close();
    return;
  }
  if (!e.ctrlKey || !S) return;
  const k = e.key.toLowerCase();
  if (k === 'm' && S) { e.preventDefault(); act('status', { muted: !S.muted }); }
  if (k === 'd' && S) { e.preventDefault(); $('btn-deaf').click(); }
  if (k === 's') { e.preventDefault(); openConnect(); }
});

// pane resizer
(() => {
  const rz = $('resizer'), pane = document.querySelector('.tree-pane');
  let start = null;
  rz.addEventListener('pointerdown', (e) => { start = { x: e.clientX, w: pane.offsetWidth }; rz.setPointerCapture(e.pointerId); });
  rz.addEventListener('pointermove', (e) => { if (start) pane.style.width = Math.max(240, Math.min(innerWidth - 380, start.w + e.clientX - start.x)) + 'px'; });
  rz.addEventListener('pointerup', () => { start = null; });
})();

document.querySelector('.logo').append(icon('lock'));
renderTabs();
renderMessages();
startEvents();
AE.init(TOKEN).then(() => { if (S) AE.apply(S); renderStatus(); }).catch((e) => toast('Audio engine failed to start: ' + e.message, 'error'));
api('init').then((r) => { if (!S) onState(r.state); }).catch((e) => toast(e.message, 'error'));

// ---------------------------------------------------------------- whisper
function whisperLists() { return (S.whisper || []).map((w) => ({ name: w.name, key: w.key, users: [...w.users], channels: [...w.channels] })); }
function saveWhisper(lists) { return act('whisper_lists', { lists }); }

function userNameByFP(fp) {
  const u = S.clients.find((c) => c.fp === fp);
  return u ? u.nick : fp.slice(0, 9) + '…';
}

function renderWhisperBar() {
  const bar = $('whisper-bar');
  bar.replaceChildren();
  if (!S.connected || !S.whisperAllowed) return;
  (S.whisper || []).slice(0, 4).forEach((w, i) => {
    const b = el('button', { class: 'tb whisper-btn', title: 'Hold to whisper to ' + w.name + (w.key > 0 ? ' (or hold ' + w.keyName + ')' : '') }, icon('whisper'), el('span', { text: w.name }));
    holdButton(b, () => { AE.whisperUI = i + 1; }, () => { AE.whisperUI = 0; });
    bar.append(b);
  });
}

async function addToWhisperList({ user, channel }) {
  const lists = whisperLists();
  let idx = 0;
  if (lists.length) {
    const s = el('select', {}, ...lists.map((w, i) => el('option', { value: i, text: w.name })), el('option', { value: -1, text: '+ New list' }));
    const ok = await new Promise((resolve) => {
      let done = false;
      modal({
        title: 'Add ' + (user ? user.nick : channel.name) + ' to whisper list', icon: 'whisper',
        body: el('div', { class: 'field' }, el('label', { text: 'Whisper list' }), s),
        buttons: [{ label: 'Cancel' }, { label: 'Add', primary: true, fn: () => { done = true; resolve(true); } }],
        onClose: () => { if (!done) resolve(false); },
      });
    });
    if (!ok) return;
    idx = +s.value;
  } else idx = -1;
  if (idx < 0) {
    lists.push({ name: user ? user.nick : channel.name, key: 0, users: [], channels: [] });
    idx = lists.length - 1;
  }
  const w = lists[idx];
  if (user && !w.users.includes(user.fp)) w.users.push(user.fp);
  if (channel && !w.channels.includes(channel.name)) w.channels.push(channel.name);
  await saveWhisper(lists);
  toast('Added to whisper list "' + w.name + '". Hold its toolbar button or hotkey to whisper.');
}

function renderWhisperTab(view) {
  const lists = whisperLists();
  const save = () => saveWhisper(lists);
  add(view,
    el('div', { class: 'callout', text: 'Whisper lets you talk to specific people or channels without leaving your channel. Only the targets receive the encryption key for a whisper — nobody else, not even the server, can listen in.' }),
    S.connected && !S.whisperAllowed && el('div', { class: 'callout warn', text: 'Whispering is disabled on this server.' }));
  lists.forEach((w, i) => {
    const name = el('input', { value: w.name, maxlength: 40 });
    name.onchange = () => { w.name = name.value; save(); };
    const keycap = el('span', { class: 'keycap', text: (S.whisper[i] && S.whisper[i].keyName) || '(none)' });
    const chip = (label, onRemove) => el('span', { class: 'chip' }, label, el('span', { class: 'x', title: 'Remove', onclick: onRemove }, icon('x')));
    const users = S.clients.filter((c) => !c.me && !w.users.includes(c.fp));
    const chans = S.channels.filter((c) => !w.channels.includes(c.name));
    const addUser = el('select', {}, el('option', { value: '', text: '+ Add user…' }), ...users.map((u) => el('option', { value: u.fp, text: u.nick })));
    addUser.onchange = () => { if (addUser.value) { w.users.push(addUser.value); save(); } };
    const addChan = el('select', {}, el('option', { value: '', text: '+ Add channel…' }), ...chans.map((c) => el('option', { value: c.name, text: c.name })));
    addChan.onchange = () => { if (addChan.value) { w.channels.push(addChan.value); save(); } };
    add(view, el('div', { class: 'wl-card' },
      el('div', { style: 'display:flex;gap:8px;align-items:center' },
        el('div', { class: 'field', style: 'flex:1;margin:0' }, name),
        keycap,
        el('button', {
          class: 'btn', disabled: !S.hotkeySupported, title: 'Set a global hotkey for this list',
          onclick: async () => { const r = await captureKey(keycap); if (r) { w.key = r.key; save(); } },
        }, icon('key'), 'Hotkey'),
        w.key > 0 && el('button', { class: 'btn', onclick: () => { w.key = 0; save(); } }, 'Clear'),
        el('button', { class: 'btn danger', title: 'Delete list', onclick: () => { lists.splice(i, 1); save(); } }, icon('trash'))),
      el('div', { class: 'chips' },
        ...w.users.map((fp, j) => chip(el('span', {}, icon('user'), userNameByFP(fp)), () => { w.users.splice(j, 1); save(); })),
        ...w.channels.map((n, j) => chip(el('span', {}, icon('hash'), n), () => { w.channels.splice(j, 1); save(); })),
        !w.users.length && !w.channels.length && el('span', { class: 'hint', text: 'No targets yet.' })),
      S.connected && el('div', { style: 'display:flex;gap:8px;margin-top:8px' }, addUser, addChan)));
  });
  add(view, el('button', {
    class: 'btn primary', style: 'margin-top:6px', onclick: () => { lists.push({ name: 'Whisper ' + (lists.length + 1), key: 0, users: [], channels: [] }); save(); },
  }, icon('plus'), 'New whisper list'),
  el('div', { class: 'hint', style: 'margin-top:10px', text: 'Users are remembered by their identity fingerprint, channels by name. ' + S.hotkeyInfo }));
}

// ---------------------------------------------------------------- avatars
function chooseAvatar() {
  const input = el('input', { type: 'file', accept: 'image/png,image/jpeg,image/webp,image/gif' });
  input.onchange = async () => {
    const f = input.files[0];
    if (!f) return;
    try {
      const url = URL.createObjectURL(f);
      const img = new Image();
      await new Promise((ok, fail) => { img.onload = ok; img.onerror = () => fail(new Error('not an image')); img.src = url; });
      const c = document.createElement('canvas');
      c.width = c.height = 128;
      const side = Math.min(img.naturalWidth, img.naturalHeight);
      c.getContext('2d').drawImage(img, (img.naturalWidth - side) / 2, (img.naturalHeight - side) / 2, side, side, 0, 0, 128, 128);
      URL.revokeObjectURL(url);
      let blob = null;
      for (const q of [0.88, 0.75, 0.6, 0.45]) {
        blob = await new Promise((r) => c.toBlob(r, 'image/webp', q));
        if (!blob || blob.type !== 'image/webp') blob = await new Promise((r) => c.toBlob(r, 'image/jpeg', q));
        if (blob && blob.size < 45000) break;
      }
      const bytes = new Uint8Array(await blob.arrayBuffer());
      let bin = '';
      for (const b of bytes) bin += String.fromCharCode(b);
      if ((await act('avatar_set', { data: btoa(bin) })) !== undefined) toast('Picture updated');
    } catch (e) {
      toast('Could not use this picture: ' + e.message, 'error');
    }
  };
  input.click();
}

// ---------------------------------------------------------------- files
function fmtBytes(n) {
  if (n < 1024) return n + ' B';
  const u = ['KB', 'MB', 'GB', 'TB'];
  let i = -1;
  do { n /= 1024; i++; } while (n >= 1024 && i < u.length - 1);
  return n.toFixed(n < 10 ? 1 : 0) + ' ' + u[i];
}

function chatTarget() {
  if (activeTab === 'channel') return { kind: 'channel', target: 0 };
  if (activeTab.startsWith('pm:')) return { kind: 'private', target: +activeTab.slice(3) };
  return null;
}

async function uploadFiles(files) {
  const t = chatTarget();
  if (!t || !S.connected) { toast('Open a channel or private chat to send files', 'warn'); return; }
  for (const f of files) {
    if (f.size > 4 * 1024 ** 3) { toast(f.name + ' is larger than 4 GB', 'error'); continue; }
    if (f.size > 50 * 1024 ** 2) toast('Preparing ' + f.name + '…');
    try {
      const q = 'kind=' + t.kind + '&target=' + t.target + '&name=' + encodeURIComponent(f.name) +
        '&mime=' + encodeURIComponent(f.type || '') + '&token=' + encodeURIComponent(TOKEN);
      const r = await fetch('/upload?' + q, { method: 'POST', body: f });
      const j = await r.json();
      if (!j.ok) throw new Error(j.error);
    } catch (e) {
      toast('Sending ' + f.name + ' failed: ' + e.message, 'error');
    }
  }
}

// Screenshots pasted from the clipboard arrive as "image.png": give them a useful name.
function namePasted(f) {
  if (f.name && !/^image\.(png|jpe?g|gif|webp|bmp)$/i.test(f.name)) return f;
  const ext = (f.type.split('/')[1] || 'png').replace('jpeg', 'jpg');
  const d = new Date(), p = (n) => String(n).padStart(2, '0');
  const stamp = `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}.${p(d.getMinutes())}.${p(d.getSeconds())}`;
  return new File([f], `Screenshot ${stamp}.${ext}`, { type: f.type });
}

function fileState(id) {
  if (!fileStates.has(id)) fileStates.set(id, { in: null, out: new Map() });
  return fileStates.get(id);
}

function onFileEvent(d) {
  const st = fileState(d.id);
  if (d.dir === 'in') {
    const was = st.in && st.in.state;
    st.in = Object.assign(st.in || {}, d);
    if (d.state === 'done' && was !== 'done' && !d.cached) { AE.sound('file'); toast('Received ' + d.name); }
    if (d.state === 'done' && d.saved) toast('Saved ' + d.name);
    if (d.state === 'failed' && !d.cached) toast('Download of ' + d.name + ' failed: ' + (d.error || 'unknown error'), 'error');
  } else {
    st.out.set(d.peerId, Object.assign(st.out.get(d.peerId) || {}, d));
  }
  document.querySelectorAll('.filecard').forEach((n) => { if (n.dataset.fileId === d.id) renderFileCard(n); });
}

const previewKind = (mime) => (/^image\/(png|jpeg|gif|webp|bmp)$/.test(mime || '') ? 'image' : /^video\/(mp4|webm)$/.test(mime || '') ? 'video' : '');
const mediaURL = (id) => '/media?id=' + encodeURIComponent(id) + '&token=' + encodeURIComponent(TOKEN);

function fileCard(f) {
  const n = el('div', { class: 'filecard' });
  n.dataset.fileId = f.id;
  n.dataset.name = f.name;
  n.dataset.size = f.size;
  n.dataset.mime = f.mime || '';
  n.dataset.own = f.own ? '1' : '';
  n.dataset.auto = f.auto ? '1' : '';
  renderFileCard(n);
  return n;
}

function progressBar(done, total) {
  const pct = total ? Math.min(100, Math.round((done / total) * 100)) : 100;
  return el('div', { class: 'progress' }, el('div', { style: 'width:' + pct + '%' }));
}

function mediaPreview(id, kind, name) {
  if (kind === 'image') {
    const img = el('img', { class: 'media-thumb', alt: name, src: mediaURL(id), title: 'Click to enlarge', loading: 'lazy' });
    img.onclick = () => openLightbox(id, kind, name);
    img.onerror = () => img.remove();
    return img;
  }
  const v = el('video', { class: 'media-thumb', src: mediaURL(id), controls: true, preload: 'metadata' });
  v.onerror = () => v.remove();
  return v;
}

function renderFileCard(n) {
  const id = n.dataset.fileId, name = n.dataset.name, size = +n.dataset.size, own = !!n.dataset.own;
  const kind = previewKind(n.dataset.mime);
  const st = fileStates.get(id) || { in: null, out: new Map() };
  const head = el('div', { class: 'fc-head' }, icon(kind === 'image' ? 'image' : kind === 'video' ? 'monitor' : 'file'),
    el('div', { class: 'fc-name' }, el('div', { text: name }), el('div', { class: 'hint', text: fmtBytes(size) + ' · end-to-end encrypted' })));
  const body = el('div', { class: 'fc-body' });
  const x = st.in;
  const showPreview = kind && (own || (x && x.state === 'done'));
  if (showPreview) body.append(mediaPreview(id, kind, name));
  if (own) {
    if (!kind && !st.out.size) body.append(el('div', { class: 'hint', text: 'Offered. Stay connected until the recipients have downloaded it.' }));
    for (const o of st.out.values()) {
      const line = el('div', { class: 'fc-line' }, el('span', { text: '→ ' + o.peer }));
      if (o.state === 'sending') line.append(progressBar(o.bytes, o.size), el('span', { class: 'hint', text: Math.round((o.bytes / Math.max(1, o.size)) * 100) + '%' }));
      else if (o.state === 'done') line.append(el('span', { class: 'ok', text: '✓ delivered' }));
      else if (o.state === 'failed') line.append(el('span', { class: 'err', text: o.error || 'failed' }));
      body.append(line);
    }
    body.append(el('div', { class: 'fc-actions' }, el('button', { class: 'btn', onclick: () => act('file_cancel', { id }) }, 'Withdraw')));
  } else if (!x || x.state === 'failed') {
    add(body, x && !x.cached && el('div', { class: 'err', text: x.error || 'failed' }),
      el('div', { class: 'fc-actions' }, el('button', { class: 'btn primary', onclick: () => act('file_download', { id }) }, icon('download'), x && !x.cached ? 'Retry' : 'Download')));
  } else if (x.state === 'requesting' || x.state === 'downloading' || x.state === 'verifying') {
    body.append(el('div', { class: 'fc-line' }, progressBar(x.bytes || 0, size),
      el('span', { class: 'hint', text: x.cached ? 'loading preview…' : x.state === 'verifying' ? 'verifying…' : x.state === 'requesting' ? 'waiting for the sender…' : fmtBytes(x.bytes || 0) })));
    if (!x.cached) body.append(el('div', { class: 'fc-actions' }, el('button', { class: 'btn', onclick: () => act('file_cancel', { id }) }, 'Cancel')));
  } else if (x.state === 'done') {
    if (x.cached && !x.saved) {
      body.append(el('div', { class: 'fc-actions' }, el('button', { class: 'btn', onclick: () => act('file_save', { id }) }, icon('download'), 'Save')));
    } else {
      if (!kind) body.append(el('div', { class: 'fc-line' }, el('span', { class: 'ok', text: '✓ Saved, checksum verified' })));
      body.append(el('div', { class: 'fc-actions' }, el('button', { class: 'btn', onclick: () => act('file_reveal', { id }) }, icon('folder'), 'Show in folder')));
    }
  }
  n.replaceChildren(head, body);
}

function openLightbox(id, kind, name) {
  const media = kind === 'image' ? el('img', { src: mediaURL(id), alt: name }) : el('video', { src: mediaURL(id), controls: true, autoplay: true });
  const st = fileStates.get(id);
  const own = !st || !st.in;
  const ov = el('div', { class: 'lightbox' },
    el('div', { class: 'lb-bar' }, el('span', { class: 'lb-name', text: name }),
      !own && el('button', { class: 'btn', onclick: (e) => { e.stopPropagation(); act('file_save', { id }).then((r) => { if (r) act('file_reveal', { id }); }); } }, icon('folder'), 'Save & show in folder'),
      el('button', { class: 'btn', onclick: () => ov.remove() }, icon('x'), 'Close')),
    el('div', { class: 'lb-media' }, media));
  ov.addEventListener('click', (e) => { if (e.target === ov || e.target.classList.contains('lb-media')) ov.remove(); });
  const esc = (e) => { if (e.key === 'Escape') { ov.remove(); document.removeEventListener('keydown', esc, true); } };
  document.addEventListener('keydown', esc, true);
  document.body.append(ov);
}

$('btn-attach').onclick = () => {
  const input = el('input', { type: 'file', multiple: true });
  input.onchange = () => uploadFiles([...input.files]);
  input.click();
};

// Paste files/screenshots anywhere (text pastes are left alone).
document.addEventListener('paste', (e) => {
  const files = [...((e.clipboardData && e.clipboardData.files) || [])];
  if (!files.length || !S || !S.connected) return;
  e.preventDefault();
  if (!chatTarget()) { toast('Open a channel or private chat to paste files', 'warn'); return; }
  uploadFiles(files.map(namePasted));
});

// Drop files anywhere on the window.
(() => {
  const hasFiles = (e) => e.dataTransfer && [...e.dataTransfer.types].includes('Files');
  let depth = 0;
  const overlay = $('drop-overlay');
  const show = (on) => {
    if (on) {
      const t = chatTarget();
      const where = !t ? 'Open a channel or private chat first' : t.kind === 'channel' ? 'Drop to send to # ' + (tabs.get('channel').title || 'channel') : 'Drop to send to ' + (tabs.get(activeTab) || {}).title;
      overlay.querySelector('.drop-text').textContent = where;
    }
    overlay.classList.toggle('hidden', !on);
  };
  document.addEventListener('dragenter', (e) => { if (hasFiles(e) && S && S.connected) { depth++; show(true); } });
  document.addEventListener('dragleave', (e) => { if (hasFiles(e) && --depth <= 0) { depth = 0; show(false); } });
  document.addEventListener('dragover', (e) => { if (hasFiles(e)) e.preventDefault(); });
  document.addEventListener('drop', (e) => {
    if (!hasFiles(e)) return;
    e.preventDefault();
    depth = 0;
    show(false);
    if (S && S.connected) uploadFiles([...e.dataTransfer.files].map(namePasted));
  });
})();

// ---------------------------------------------------------------- server password
function openServerPassword() {
  const pw = el('input', { type: 'password', maxlength: 128, autocomplete: 'new-password' });
  const pw2 = el('input', { type: 'password', maxlength: 128, autocomplete: 'new-password' });
  modal({
    title: 'Server password', icon: 'lock',
    body: [
      el('div', { class: 'callout' + (S.serverHasPw ? '' : ' warn'), text: S.serverHasPw ? 'This server requires a password to connect.' : 'This server has no password: anyone who knows the address can connect.' }),
      el('div', { class: 'field' }, el('label', { text: S.serverHasPw ? 'New password' : 'Password' }), pw),
      el('div', { class: 'field' }, el('label', { text: 'Repeat password' }), pw2),
      el('div', { class: 'hint', text: 'People who are connected stay connected. Share the password privately with anyone who should be able to join.' }),
    ],
    buttons: [
      { label: 'Cancel' },
      S.serverHasPw && {
        label: 'Remove password', danger: true, fn: async () => {
          if (!(await confirmModal('Remove server password', 'Anyone who knows the server address will be able to connect. Continue?', 'Remove', true))) return false;
          return act('server_password', { remove: true });
        },
      },
      {
        label: S.serverHasPw ? 'Change password' : 'Set password', primary: true, fn: async () => {
          if (!pw.value) { toast('Enter a password', 'error'); return false; }
          if (pw.value !== pw2.value) { toast('The passwords do not match', 'error'); return false; }
          return (await act('server_password', { password: pw.value })) === undefined ? false : true;
        },
      },
    ].filter(Boolean),
  });
}

// ---------------------------------------------------------------- screen share UI
function renderShareButton() {
  const b = $('btn-share');
  const allowed = S && S.connected && (can('screen.share') || SS.sharing);
  b.classList.toggle('hidden', !allowed);
  btnContent(b, 'monitor', SS.sharing ? (SS.withAudio ? 'Stop sharing (with sound)' : 'Stop sharing') : 'Share screen');
  b.classList.toggle('sharing', SS.sharing);
}

async function toggleShare() {
  try {
    if (SS.sharing) await SS.stop();
    else {
      await SS.start();
      if (SS.withAudio) {
        toast('You are sharing your screen with sound. People in your channel can click LIVE to watch.');
        if (!SS.ownAudioFiltered) toast('Note: this browser engine cannot keep SecureSpeak\'s own sound out of the shared audio, so viewers may hear voices from the call through your share.', 'warn');
      } else {
        toast('You are sharing your screen without sound. ' + (S.platform === 'windows'
          ? 'To include sound, stop and share again and switch on “Also share system audio” in the sharing dialog (available for screens and windows).'
          : 'On Linux, sound can only be shared together with a browser tab, not with a screen or window.'), 'warn');
      }
    }
  } catch (e) {
    if (e && e.name !== 'NotAllowedError' && e.name !== 'AbortError') toast('Could not share: ' + e.message, 'error');
  }
  renderShareButton();
}

function watchUser(u) {
  SS.watch(u.id);
  renderStage();
}

function renderStage() {
  const stage = $('stage');
  const u = SS.watching && userById(SS.watching);
  if (!u) { stage.classList.add('hidden'); stage.replaceChildren(); return; }
  if (stage.dataset.for === String(u.id) && !stage.classList.contains('hidden')) return; // keep the canvas
  stage.dataset.for = String(u.id);
  const canvas = el('canvas', { id: 'stage-canvas' });
  const fs = el('button', { class: 'btn', onclick: () => canvas.requestFullscreen && canvas.requestFullscreen() }, icon('expand'), 'Fullscreen');
  const close = el('button', { class: 'btn', onclick: () => { SS.unwatch(); renderStage(); } }, icon('x'), 'Stop watching');
  const volVal = el('span', { class: 'val', text: Math.round(SS.shareVolume * 100) + '%' });
  const vol = el('input', { type: 'range', min: 0, max: 200, step: 5, value: Math.round(SS.shareVolume * 100), title: 'Volume of the shared sound' });
  vol.oninput = () => { SS.shareVolume = vol.value / 100; volVal.textContent = vol.value + '%'; SS.applyShareVolume(); };
  const mute = el('button', { class: 'icon-btn', title: 'Mute the shared sound' }, icon(SS.shareMuted ? 'volX' : 'vol'));
  mute.onclick = () => { SS.shareMuted = !SS.shareMuted; mute.replaceChildren(icon(SS.shareMuted ? 'volX' : 'vol')); SS.applyShareVolume(); };
  const audioCtl = el('span', { class: 'stage-audio' }, mute, vol, volVal);
  stage.replaceChildren(
    el('div', { class: 'stage-bar' }, el('span', { class: 'badge live', text: 'LIVE' }), el('span', { class: 'stage-title', text: u.nick + '’s screen' }),
      el('span', { class: 'hint', style: 'margin:0', text: 'end-to-end encrypted' }), el('span', { class: 'spacer' }), audioCtl, fs, close),
    el('div', { class: 'stage-view' }, canvas, el('div', { id: 'stage-wait', class: 'hint', text: 'Connecting to the screen share…' })));
  stage.classList.remove('hidden');
}

SS.onChange = () => { renderShareButton(); renderStage(); renderTree(); };
$('btn-share').onclick = toggleShare;

// ---------------------------------------------------------------- roles & permissions
let roleMgr = null;

// roles I can hand out / edit: below my power (the owner can do everything)
const roleEditable = (r) => S.owner || r.power < S.power;

function sortedRoles() { return [...(S.roles || [])].sort((a, b) => b.power - a.power || a.id - b.id); }

function openAssignRoles(u, member) {
  // u: online user (uiClient) or null; member: {key, nick, roles} for offline identities
  const target = u || member;
  const current = new Set(target.roles || []);
  const boxes = [];
  const list = el('div', { class: 'role-checks' });
  for (const r of sortedRoles()) {
    if (r.default) continue;
    const cb = el('input', { type: 'checkbox', checked: current.has(r.id), disabled: !roleEditable(r) });
    boxes.push([r, cb]);
    const dot = el('span', { class: 'role-dot' });
    if (safeColor(r.color)) dot.style.background = safeColor(r.color);
    list.append(el('label', { class: 'check' + (roleEditable(r) ? '' : ' disabled'), title: roleEditable(r) ? '' : 'This role has the same or more power than you' },
      cb, dot, el('span', { text: r.name }), el('span', { class: 'hint', style: 'margin:0 0 0 auto', text: 'power ' + r.power })));
  }
  if (!boxes.length) list.append(el('div', { class: 'hint', text: 'There are no roles yet. Create one under Roles & permissions.' }));
  modal({
    title: 'Roles of ' + target.nick, icon: 'shield',
    body: [list, el('div', { class: 'hint', style: 'margin-top:10px', text: 'Roles are linked to this person\'s identity key and apply automatically whenever they connect. Everyone also has the default role.' })],
    buttons: [{ label: 'Cancel' }, {
      label: 'Save', primary: true, fn: () => {
        const roles = boxes.filter(([, cb]) => cb.checked).map(([r]) => r.id);
        return act('client_roles', u ? { id: u.id, roles } : { key: member.key, roles });
      },
    }],
  });
}

function showNewToken(d) {
  const r = roleById(d.role);
  const code = el('div', { class: 'fp token', text: d.token });
  const copy = el('button', { class: 'btn', type: 'button' }, 'Copy');
  copy.onclick = async () => {
    try { await navigator.clipboard.writeText(d.token); copy.textContent = 'Copied ✓'; } catch (e) { toast('Copy failed; select the key manually', 'warn'); }
  };
  modal({
    title: 'New privilege key', icon: 'key',
    body: [
      el('div', { text: 'Give this key to the person who should get the role "' + (r ? r.name : '?') + '"' + (d.note ? ' (' + d.note + ')' : '') + '. It works once; after that the role is tied to their identity.' }),
      el('div', { style: 'display:flex;gap:8px;align-items:center;margin-top:12px' }, code, copy),
      el('div', { class: 'callout warn', text: 'This key is shown only now. Anyone who has it can claim the role, so share it privately.' }),
    ],
  });
  if (roleMgr) act('token_list');
}

function openRoleManager(initial) {
  let tab = initial || (can('roles.manage') ? 'roles' : can('roles.assign') ? 'members' : 'keys');
  let selRole = null;
  let draft = null;
  let members = null, tokens = null;
  const tabsBar = el('div', { class: 'stabs' });
  const view = el('div');

  const render = () => {
    tabsBar.replaceChildren(...[
      can('roles.manage') && ['roles', 'Roles'],
      can('roles.assign') && ['members', 'Members'],
      can('tokens.create') && ['keys', 'Privilege keys'],
    ].filter(Boolean).map(([k, l]) => {
      const b = el('button', { class: k === tab ? 'on' : '', text: l });
      b.onclick = () => { tab = k; render(); if (k === 'members') act('member_list'); if (k === 'keys') act('token_list'); };
      return b;
    }));
    view.replaceChildren();
    if (tab === 'roles') renderRoles();
    else if (tab === 'members') renderMembers();
    else renderKeys();
  };

  function renderRoles() {
    const roles = sortedRoles();
    if (selRole !== -1 && (!selRole || !(S.roles || []).some((r) => r.id === selRole))) selRole = roles.length ? roles[0].id : 0;
    if (selRole !== -1 && (!draft || draft.id !== selRole)) {
      const r = roleById(selRole);
      draft = r ? JSON.parse(JSON.stringify(r)) : null;
    }
    const left = el('div', { class: 'role-list' });
    for (const r of roles) {
      const dot = el('span', { class: 'role-dot' });
      if (safeColor(r.color)) dot.style.background = safeColor(r.color);
      const it = el('div', { class: 'role-item' + (r.id === selRole ? ' on' : '') }, dot,
        el('span', { class: 'n', text: r.name }), el('span', { class: 'hint', style: 'margin:0', text: r.default ? 'everyone' : String(r.power) }));
      it.onclick = () => { selRole = r.id; draft = null; render(); };
      left.append(it);
    }
    const newBtn = el('button', { class: 'btn', style: 'margin-top:8px;width:100%' }, icon('plus'), 'New role');
    newBtn.onclick = () => {
      selRole = -1;
      draft = { id: 0, name: 'New role', color: '#5b7cfa', power: Math.max(0, Math.min(S.owner ? 10 : S.power - 1, 10)), perms: [] };
      render();
    };
    left.append(newBtn);

    const right = el('div', { class: 'role-edit' });
    if (draft) {
      const editable = draft.id === 0 || roleEditable(draft);
      const name = el('input', { value: draft.name, maxlength: 32, disabled: !editable });
      name.oninput = () => { draft.name = name.value; };
      const color = el('input', { type: 'color', value: safeColor(draft.color) || '#8b95a7', disabled: !editable });
      color.oninput = () => { draft.color = color.value; };
      const power = el('input', { type: 'number', min: 0, max: S.owner ? 1000 : S.power - 1, value: draft.power, disabled: !editable || draft.default });
      power.oninput = () => { draft.power = +power.value || 0; };
      add(right,
        !editable && el('div', { class: 'callout', text: 'This role has the same or more power than you, so you cannot change it.' }),
        draft.default && el('div', { class: 'callout', text: 'The default role applies to everyone, including people without any assigned role.' }),
        el('div', { class: 'grid2', style: 'grid-template-columns:1fr 70px 110px' },
          el('div', { class: 'field' }, el('label', { text: 'Name' }), name),
          el('div', { class: 'field' }, el('label', { text: 'Color' }), color),
          el('div', { class: 'field' }, el('label', { text: 'Power' }), power)),
        el('div', { class: 'hint', style: 'margin:-6px 0 8px', text: 'Users can only kick, ban, move or change roles of users with lower power, and only hand out roles below their own power.' + (S.owner ? '' : ' Your power: ' + S.power + '.') }));
      let group = '';
      const perms = new Set(draft.perms);
      const grid = el('div', { class: 'perm-grid' });
      for (const p of S.permCatalog) {
        if (p.group !== group) { group = p.group; grid.append(el('div', { class: 'section-title', style: 'grid-column:1/-1;margin:10px 0 2px', text: group })); }
        const mine = S.owner || S.perms.includes(p.id);
        const cb = el('input', { type: 'checkbox', checked: perms.has(p.id), disabled: !editable || !mine });
        cb.onchange = () => { if (cb.checked) perms.add(p.id); else perms.delete(p.id); draft.perms = [...perms]; };
        grid.append(el('label', { class: 'check' + (mine ? '' : ' disabled'), title: mine ? p.id : 'You cannot grant a permission you do not have' }, cb, p.label));
      }
      right.append(grid);
      if (editable) {
        const save = el('button', { class: 'btn primary' }, draft.id === 0 ? 'Create role' : 'Save role');
        save.onclick = async () => {
          const res = await act('role_save', { id: draft.id, name: draft.name, color: draft.color, power: draft.power, perms: draft.perms });
          if (res !== undefined) { toast('Role saved'); if (draft.id === 0) { const nm = draft.name; setTimeout(() => { const r = (S.roles || []).find((x) => x.name === nm); if (r) { selRole = r.id; draft = null; render(); } }, 300); } }
        };
        const del = !draft.default && draft.id !== 0 && el('button', { class: 'btn danger' }, icon('trash'), 'Delete role');
        if (del) del.onclick = async () => {
          if (await confirmModal('Delete role', 'Delete the role "' + draft.name + '"? Everyone loses it, and unused privilege keys for it become invalid.', 'Delete', true)) {
            await act('role_delete', { id: draft.id });
            selRole = null; draft = null;
          }
        };
        add(right, el('div', { class: 'actions', style: 'margin-top:14px' }, save, del));
      }
    }
    view.append(el('div', { class: 'role-layout' }, left, right));
  }

  function renderMembers() {
    if (!members) { view.append(el('div', { class: 'hint', text: 'Loading…' })); return; }
    add(view, el('div', { class: 'hint', style: 'margin:0 0 8px', text: 'Everyone with at least one role. Roles follow the identity key, so they also apply when the person is offline or changes nickname.' }));
    if (!members.length) view.append(el('div', { class: 'hint', text: 'No members with roles yet.' }));
    const tbl = el('table', { class: 'list' }, el('tr', {}, ...['', 'Nickname', 'Roles', 'Identity', ''].map((h) => el('th', { text: h }))));
    for (const m of members) {
      const fake = { owner: m.owner, roles: m.roles };
      const online = m.online && S.clients.find((c) => c.fp === m.fp);
      const power = m.owner ? Infinity : Math.max(0, ...(m.roles || []).map((id) => (roleById(id) || { power: 0 }).power));
      const editable = !m.owner && (S.owner || power < S.power) && m.fp !== S.identity;
      const btn = editable && el('button', { class: 'btn', onclick: () => (online ? openAssignRoles(online) : openAssignRoles(null, { key: m.key, nick: m.nick, roles: m.roles })) }, 'Edit roles');
      tbl.append(el('tr', {},
        el('td', {}, el('span', { class: 'dot' + (m.online ? ' on' : ''), title: m.online ? 'online' : 'offline' })),
        el('td', { text: m.nick || '?' }),
        el('td', {}, el('span', { style: 'display:inline-flex;gap:4px;flex-wrap:wrap' }, ...roleBadges(fake, 5))),
        el('td', { class: 'mono', style: 'font-size:11px', text: m.fp.slice(0, 19) + '…', title: m.fp }),
        el('td', {}, btn || (m.owner ? el('span', { class: 'hint', text: 'protected' }) : ''))));
    }
    if (members.length) view.append(tbl);
  }

  function renderKeys() {
    const assignable = sortedRoles().filter((r) => !r.default && roleEditable(r));
    const roleSel = el('select', {}, ...assignable.map((r) => el('option', { value: r.id, text: r.name })));
    const note = el('input', { placeholder: 'Note, e.g. who it is for', maxlength: 80 });
    const create = el('button', { class: 'btn primary', disabled: !assignable.length }, icon('key'), 'Create key');
    create.onclick = () => act('token_create', { role: +roleSel.value, note: note.value });
    add(view,
      el('div', { class: 'hint', style: 'margin:0 0 8px', text: 'A privilege key gives one person a role. It can be used once; after that the role is tied to their identity.' }),
      el('div', { class: 'key-create' }, roleSel, note, create),
      el('div', { class: 'section-title', text: 'Unused keys' }));
    if (!tokens) { view.append(el('div', { class: 'hint', text: 'Loading…' })); return; }
    if (!tokens.length) { view.append(el('div', { class: 'hint', text: 'No unused keys.' })); return; }
    const tbl = el('table', { class: 'list' }, el('tr', {}, ...['Role', 'Note', 'Created', 'By', ''].map((h) => el('th', { text: h }))));
    for (const t of tokens) {
      const r = roleById(t.role);
      const del = el('button', { class: 'btn', text: 'Revoke' });
      del.onclick = () => act('token_delete', { id: t.id });
      tbl.append(el('tr', {}, el('td', { text: t.owner ? 'Owner' : (r ? r.name : '?') }), el('td', { text: t.note || '' }),
        el('td', { text: new Date(t.created * 1000).toLocaleString() }), el('td', { text: t.by || '' }), el('td', {}, del)));
    }
    view.append(tbl);
  }

  roleMgr = {
    refresh: () => { if (!(document.activeElement && view.contains(document.activeElement) && /INPUT|SELECT/.test(document.activeElement.tagName))) render(); },
    members: (m) => { members = m; if (tab === 'members') render(); },
    tokens: (t) => { tokens = t; if (tab === 'keys') render(); },
  };
  const h = modal({ title: 'Roles & permissions', icon: 'shield', wide: true, body: [tabsBar, el('div', { style: 'height:12px' }), view], onClose: () => { roleMgr = null; } });
  h.ov.querySelector('.body').style.paddingTop = '0';
  h.ov.querySelector('.modal').classList.add('xwide');
  render();
  if (can('roles.assign')) act('member_list');
  if (can('tokens.create')) act('token_list');
}

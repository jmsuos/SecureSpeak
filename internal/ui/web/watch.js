'use strict';
// Watch YouTube together. The person who starts a video is the host: their
// player has YouTube's controls, and every play, pause or seek is sent to the
// channel (end-to-end encrypted, message kind "wt"). Viewers follow the host.
//
// The video plays in YouTube's official embedded player. Ads are YouTube's
// business; signing in to YouTube Premium in the app window removes them.
// The player is controlled through YouTube's documented postMessage
// interface, so no third-party script ever runs in the app itself.

const YT_ORIGIN = 'https://www.youtube.com';

const W = {
  session: null, // {host, hostNick, video, title, playing, t, at}
  joined: false,
  iframe: null,
  player: { ready: false, state: -1, time: 0, title: '' },
  volume: 80,
  muted: false,
  onChange: null, // UI hook
  send: null, // (data) => Promise, set by app.js
  me: () => 0, // my client id
  nickOf: () => '',
  heartbeat: 0,
  listenTimer: 0,
  lastSent: { t: 0, p: false, at: 0 },

  // Accepts youtube.com/watch?v=…, youtu.be/…, /shorts/…, /embed/…, /live/… or a bare id.
  parseVideo(input) {
    const s = String(input || '').trim();
    if (/^[A-Za-z0-9_-]{11}$/.test(s)) return s;
    let u;
    try { u = new URL(/^https?:\/\//i.test(s) ? s : 'https://' + s); } catch (e) { return null; }
    const host = u.hostname.replace(/^(www\.|m\.|music\.)/, '');
    let id = null;
    if (host === 'youtu.be') id = u.pathname.slice(1).split('/')[0];
    else if (host === 'youtube.com' || host === 'youtube-nocookie.com') {
      id = u.searchParams.get('v');
      const m = u.pathname.match(/^\/(?:shorts|embed|live|v)\/([^/?#]+)/);
      if (!id && m) id = m[1];
    }
    return id && /^[A-Za-z0-9_-]{11}$/.test(id) ? id : null;
  },

  isHost() { return !!this.session && this.session.host === this.me(); },

  // expected position of the shared video right now
  expected() {
    const s = this.session;
    if (!s) return 0;
    return s.t + (s.playing ? (performance.now() - s.at) / 1000 : 0);
  },

  changed() { if (this.onChange) this.onChange(); },

  // ---- host
  start(video) {
    this.session = { host: this.me(), hostNick: this.nickOf(this.me()), video, title: '', playing: true, t: 0, at: performance.now() };
    this.broadcast('start');
    this.join();
    clearInterval(this.heartbeat);
    this.heartbeat = setInterval(() => { if (this.isHost()) this.broadcast('state'); }, 5000);
  },

  stop() {
    if (this.isHost()) this.send({ op: 'stop' });
    this.end();
  },

  broadcast(op) {
    const s = this.session;
    if (!s || !this.isHost()) return;
    const t = this.player.ready ? this.player.time : this.expected();
    this.lastSent = { t, p: s.playing, at: performance.now() };
    this.send({ op, v: s.video, t: Math.max(0, +t.toFixed(2)), p: s.playing, title: (s.title || '').slice(0, 200) });
  },

  // ---- everyone
  onMessage(from, fromNick, d) {
    if (!d || typeof d !== 'object') return;
    if (d.op === 'hello') {
      if (this.isHost()) this.broadcast('state');
      return;
    }
    if (d.op === 'stop') {
      if (this.session && this.session.host === from) {
        this.end();
        toast(fromNick + ' stopped the video.');
      }
      return;
    }
    if (d.op !== 'start' && d.op !== 'state') return;
    const video = this.parseVideo(d.v);
    if (!video) return;
    const t = Math.max(0, Number(d.t) || 0);
    const s = this.session;
    const isNew = !s || s.host !== from || s.video !== video;
    if (s && s.host !== from && d.op === 'state') return; // someone else's leftover heartbeat
    if (isNew && this.isHost()) clearInterval(this.heartbeat); // someone else took over
    this.session = { host: from, hostNick: fromNick, video, title: String(d.title || '').slice(0, 200), playing: !!d.p, t, at: performance.now() };
    if (isNew) {
      if (this.joined) this.load(); // follow the new video
      else if (d.op === 'start' || !s) {
        actionToast(fromNick + ' started a YouTube video' + (d.title ? ': “' + String(d.title).slice(0, 80) + '”' : '') + '.', [
          { label: 'Watch together', primary: true, fn: () => this.join() },
        ]);
      }
    }
    if (this.joined) this.sync();
    this.changed();
  },

  join() {
    if (!this.session) return;
    this.joined = true;
    if (!this.isHost()) this.send({ op: 'hello' });
    this.load();
    this.changed();
  },

  leave() {
    this.joined = false;
    this.unload();
    this.changed();
  },

  end() {
    clearInterval(this.heartbeat);
    this.session = null;
    this.joined = false;
    this.unload();
    this.changed();
  },

  // The host left the channel or we disconnected: end the session.
  check(inChannel) {
    if (!this.session) return;
    if (!inChannel(this.session.host) && !this.isHost()) { this.end(); return; }
  },

  // ---- player
  embedURL(video, host) {
    const p = new URLSearchParams({
      enablejsapi: '1', autoplay: '1', playsinline: '1', rel: '0',
      controls: host ? '1' : '0', disablekb: host ? '0' : '1', fs: '1',
      origin: location.origin,
    });
    return YT_ORIGIN + '/embed/' + video + '?' + p.toString();
  },

  load() {
    this.unload();
    const s = this.session;
    if (!s) return;
    this.player = { ready: false, state: -1, time: 0, title: '' };
    const f = document.createElement('iframe');
    f.src = this.embedURL(s.video, this.isHost());
    f.allow = 'autoplay; encrypted-media; picture-in-picture; fullscreen';
    f.referrerPolicy = 'strict-origin-when-cross-origin';
    f.allowFullscreen = true;
    f.className = 'yt-frame';
    f.addEventListener('load', () => this.listen());
    this.iframe = f;
    this.changed();
  },

  unload() {
    clearInterval(this.listenTimer);
    if (this.iframe) this.iframe.remove();
    this.iframe = null;
    this.player = { ready: false, state: -1, time: 0, title: '' };
  },

  post(msg) {
    if (this.iframe && this.iframe.contentWindow) this.iframe.contentWindow.postMessage(JSON.stringify(msg), YT_ORIGIN);
  },
  cmd(func, args = []) { this.post({ event: 'command', func, args, id: 1, channel: 'widget' }); },

  // Ask the player to report its state; repeat until it answers.
  listen() {
    clearInterval(this.listenTimer);
    let n = 0;
    const ping = () => { this.post({ event: 'listening', id: 1, channel: 'widget' }); if (++n > 40 || this.player.ready) clearInterval(this.listenTimer); };
    ping();
    this.listenTimer = setInterval(ping, 250);
  },

  onPlayerMessage(e) {
    if (e.origin !== YT_ORIGIN || !this.iframe || e.source !== this.iframe.contentWindow) return;
    let d;
    try { d = typeof e.data === 'string' ? JSON.parse(e.data) : e.data; } catch (err) { return; }
    if (!d || typeof d !== 'object') return;
    const p = this.player;
    if (d.event === 'onReady' || d.event === 'initialDelivery' || d.event === 'infoDelivery') {
      const first = !p.ready;
      p.ready = true;
      const info = d.info || {};
      if (typeof info.currentTime === 'number') p.time = info.currentTime;
      if (info.videoData && info.videoData.title) p.title = String(info.videoData.title);
      if (typeof info.playerState === 'number') this.onState(info.playerState);
      if (first) this.onReady();
      else if (this.isHost()) this.hostWatch();
    } else if (d.event === 'onStateChange' && typeof d.info === 'number') {
      this.onState(d.info);
    } else if (d.event === 'onError') {
      const code = Number(d.info);
      toast(code === 101 || code === 150 || code === 153
        ? 'This video can’t be played outside YouTube (the owner disabled embedding).'
        : 'YouTube could not play this video (error ' + code + ').', 'error');
    }
  },

  onReady() {
    this.applyVolume();
    if (this.isHost()) {
      if (this.player.title && !this.session.title) { this.session.title = this.player.title; this.broadcast('state'); }
      this.cmd('playVideo');
    } else {
      this.sync(true);
    }
    this.changed();
  },

  onState(state) {
    const p = this.player;
    if (state === p.state) return;
    p.state = state;
    const s = this.session;
    if (!s) return;
    if (this.isHost()) {
      // 1 playing, 2 paused, 0 ended; 3 buffering keeps the previous intent
      if (state === 1 || state === 2 || state === 0) {
        s.playing = state === 1;
        s.t = p.time; s.at = performance.now();
        if (p.title && !s.title) s.title = p.title;
        this.broadcast('state');
      }
    } else if (state === 1 && !s.playing) {
      this.cmd('pauseVideo'); // viewers don't control playback
    } else if (state === 2 && s.playing) {
      this.cmd('playVideo');
    }
    this.changed();
  },

  // host: detect seeks (a jump compared to where the video should be)
  hostWatch() {
    const s = this.session, p = this.player;
    if (!s) return;
    const want = s.t + (s.playing ? (performance.now() - s.at) / 1000 : 0);
    if (Math.abs(p.time - want) > 2) {
      s.t = p.time; s.at = performance.now();
      this.broadcast('state');
    }
  },

  // viewer: follow the host
  sync(force) {
    if (!this.joined || this.isHost() || !this.player.ready || !this.session) return;
    const want = this.expected();
    if (force || Math.abs(this.player.time - want) > 1.5) {
      this.cmd('seekTo', [Math.max(0, want), true]);
      this.player.time = want;
    }
    this.cmd(this.session.playing ? 'playVideo' : 'pauseVideo');
  },

  setVolume(v) { this.volume = v; this.applyVolume(); },
  setMuted(m) { this.muted = m; this.applyVolume(); },
  applyVolume() {
    this.cmd('setVolume', [this.volume]);
    this.cmd(this.muted ? 'mute' : 'unMute');
  },
};
window.addEventListener('message', (e) => W.onPlayerMessage(e));
window.W = W;

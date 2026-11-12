'use strict';
// SecureSpeak screen share.
//
// Sharing: getDisplayMedia -> VideoFrames (MediaStreamTrackProcessor, or a
// canvas fallback) -> WebCodecs VP9 (VP8 fallback) -> local WebSocket ->
// core, which encrypts with the channel voice key, signs and sends it.
// Watching: core delivers decrypted frames for the sharer we subscribed to
// -> VideoDecoder -> canvas.

const VCODEC = { 11: 'vp09.00.10.08', 10: 'vp8' };
const MAX_FRAME = 900 * 1024;

const SS = {
  // ---- sharing
  track: null, encoder: null, codecId: 0, w: 0, h: 0, fps: 15, count: 0, forceKey: false,
  timer: null, reader: null, lastTs: 0, sent: 0, sharing: false,
  // ---- watching
  watching: 0, decoder: null, decCodec: 0, waitKey: true, lastKeyReq: 0, ts: 0, frames: 0,
  onChange: null,

  async start() {
    if (this.sharing) return;
    if (typeof VideoEncoder === 'undefined') throw new Error('Screen sharing needs a browser engine with WebCodecs (Edge/Chrome 94+).');
    const stream = await navigator.mediaDevices.getDisplayMedia({
      video: { frameRate: { ideal: 15, max: 30 }, width: { max: 1920 }, height: { max: 1080 } }, audio: false,
    });
    this.track = stream.getVideoTracks()[0];
    try { this.track.contentHint = 'detail'; } catch (e) { /* optional */ }
    this.track.addEventListener('ended', () => this.stop());
    this.sharing = true;
    this.count = 0;
    this.forceKey = true;
    await api('share', { on: true });
    this.pump();
    this.changed();
  },

  async stop() {
    if (!this.sharing) return;
    this.sharing = false;
    if (this.timer) { clearInterval(this.timer); this.timer = null; }
    if (this.reader) { try { this.reader.cancel(); } catch (e) { /* done */ } this.reader = null; }
    if (this.track) { this.track.stop(); this.track = null; }
    if (this.encoder) { try { this.encoder.close(); } catch (e) { /* closed */ } this.encoder = null; }
    if (this._video) { this._video.srcObject = null; this._video = null; }
    act('share', { on: false });
    this.changed();
  },

  changed() { if (this.onChange) this.onChange(); },

  async pump() {
    if (window.MediaStreamTrackProcessor) {
      const proc = new MediaStreamTrackProcessor({ track: this.track });
      this.reader = proc.readable.getReader();
      while (this.sharing) {
        let r;
        try { r = await this.reader.read(); } catch (e) { break; }
        if (r.done) break;
        await this.handle(r.value);
      }
    } else {
      // fallback: draw the captured video onto a canvas at the target rate
      const video = document.createElement('video');
      video.muted = true;
      video.srcObject = new MediaStream([this.track]);
      await video.play();
      this._video = video;
      const canvas = document.createElement('canvas');
      const ctx = canvas.getContext('2d');
      this.timer = setInterval(() => {
        if (!video.videoWidth) return;
        canvas.width = video.videoWidth; canvas.height = video.videoHeight;
        ctx.drawImage(video, 0, 0);
        this.handle(new VideoFrame(canvas, { timestamp: Math.round(performance.now() * 1000) }));
      }, 1000 / this.fps);
    }
  },

  async configure(w, h) {
    if (this.encoder) { try { this.encoder.close(); } catch (e) { /* closed */ } }
    this.encoder = null;
    for (const id of [11, 10]) {
      const cfg = {
        codec: VCODEC[id], width: w, height: h, framerate: this.fps,
        bitrate: Math.min(4_000_000, Math.max(800_000, Math.round(w * h * 1.4))),
        latencyMode: 'realtime',
      };
      let ok = false;
      try { ok = (await VideoEncoder.isConfigSupported(cfg)).supported; } catch (e) { ok = false; }
      if (!ok) continue;
      const enc = new VideoEncoder({
        output: (chunk) => this.onChunk(chunk, id),
        error: (e) => { console.warn('video encoder', e); this.encoder = null; },
      });
      enc.configure(cfg);
      this.encoder = enc; this.codecId = id; this.w = w; this.h = h; this.forceKey = true;
      return;
    }
    throw new Error('No VP9/VP8 encoder available');
  },

  async handle(frame) {
    try {
      if (!this.sharing) return;
      const ts = frame.timestamp;
      if (ts - this.lastTs < 1e6 / this.fps - 2000) return; // limit frame rate
      this.lastTs = ts;
      const w = frame.displayWidth & ~1, h = frame.displayHeight & ~1;
      if (!this.encoder || this.encoder.state !== 'configured' || w !== this.w || h !== this.h) await this.configure(w, h);
      if (this.encoder.encodeQueueSize > 2) return; // encoder busy: skip frame
      const key = this.forceKey || this.count % 300 === 0; // periodic keyframe every ~20 s
      this.forceKey = false;
      this.encoder.encode(frame, { keyFrame: key });
      this.count++;
    } catch (e) {
      console.warn('screen share', e);
    } finally {
      frame.close();
    }
  },

  onChunk(chunk, codecId) {
    if (chunk.byteLength > MAX_FRAME) { this.forceKey = true; return; }
    const msg = new Uint8Array(3 + chunk.byteLength);
    msg[0] = 2;
    msg[1] = chunk.type === 'key' ? 1 : 0;
    msg[2] = codecId;
    chunk.copyTo(msg.subarray(3));
    if (AE.ws && AE.ws.readyState === 1) { AE.ws.send(msg); this.sent++; }
  },

  requestKeyframe() { this.forceKey = true; },

  // ---- watching
  watch(id) {
    if (this.watching && this.watching !== id) this.unwatch(true);
    this.watching = id;
    this.waitKey = true;
    this.frames = 0;
    act('watch', { id, on: true });
    this.changed();
  },

  unwatch(silent) {
    if (!this.watching) return;
    act('watch', { id: this.watching, on: false });
    this.watching = 0;
    if (this.decoder) { try { this.decoder.close(); } catch (e) { /* closed */ } this.decoder = null; }
    if (!silent) this.changed();
  },

  askKey() {
    const now = performance.now();
    if (now - this.lastKeyReq > 1000) { this.lastKeyReq = now; act('request_keyframe', { id: this.watching }); }
  },

  // [2][sender u32][flags][codec][payload]
  onFrame(b) {
    const sender = new DataView(b.buffer, b.byteOffset).getUint32(1);
    if (sender !== this.watching || typeof VideoDecoder === 'undefined') return;
    const key = (b[5] & 1) === 1, codec = b[6];
    if (!VCODEC[codec]) return;
    if (!this.decoder || this.decoder.state === 'closed' || this.decCodec !== codec) {
      if (!key) { this.askKey(); return; }
      this.decoder = new VideoDecoder({
        output: (f) => this.draw(f),
        error: (e) => { console.warn('video decoder', e); this.decoder = null; this.waitKey = true; this.askKey(); },
      });
      this.decoder.configure({ codec: VCODEC[codec], optimizeForLatency: true });
      this.decCodec = codec;
    }
    if (this.waitKey && !key) { this.askKey(); return; }
    this.waitKey = false;
    if (this.decoder.decodeQueueSize > 6) { this.waitKey = true; this.askKey(); return; } // fell behind
    try {
      this.decoder.decode(new EncodedVideoChunk({ type: key ? 'key' : 'delta', timestamp: (this.ts++) * 66666, data: b.subarray(7) }));
    } catch (e) {
      this.decoder = null; this.waitKey = true; this.askKey();
    }
  },

  draw(frame) {
    const c = document.getElementById('stage-canvas');
    if (c) {
      if (c.width !== frame.displayWidth || c.height !== frame.displayHeight) { c.width = frame.displayWidth; c.height = frame.displayHeight; }
      c.getContext('2d').drawImage(frame, 0, 0);
      this.frames++;
      const ph = document.getElementById('stage-wait');
      if (ph) ph.remove();
    }
    frame.close();
  },
};
window.SS = SS;

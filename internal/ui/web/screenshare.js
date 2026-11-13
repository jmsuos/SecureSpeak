'use strict';
// SecureSpeak screen share.
//
// Sharing: getDisplayMedia -> VideoFrames (MediaStreamTrackProcessor, or a
// canvas fallback) -> WebCodecs encoder -> local WebSocket -> core, which
// encrypts with the channel voice key, signs and sends it.
// Encoder choice: H.264 on the GPU (NVENC, Quick Sync, AMF...), then VP9 on
// the GPU, then VP9/VP8 in software, limited to codecs every viewer can
// decode (viewers report that when they start watching).
// Watching: core delivers decrypted frames for the sharer we subscribed to
// -> VideoDecoder -> canvas.

const VCODEC = { 13: 'avc1.640034', 11: 'vp09.00.10.08', 10: 'vp8' }; // decoder strings
const VNAME = { 13: 'H.264', 11: 'VP9', 10: 'VP8' };
const LEGACY_CODECS = [10, 11]; // viewers that don't report codecs (older versions)

// Quality presets; "custom" uses the user's own numbers.
const SHARE_PRESETS = {
  sharp: { fps: 15, height: 1080, mbps: 0, hint: 'detail' }, // 0 = automatic (by resolution, max 4 Mbit/s)
  smooth: { fps: 30, height: 1080, mbps: 6, hint: 'motion' },
  gaming: { fps: 60, height: 1080, mbps: 10, hint: 'motion' },
};

// H.264 level for the resolution and frame rate (High profile).
function avcCodec(w, h, fps) {
  const mbs = Math.ceil(w / 16) * Math.ceil(h / 16), rate = mbs * fps;
  if (mbs <= 8704 && rate <= 522240) return 'avc1.64002A'; // 4.2: 1080p60
  if (mbs <= 22080 && rate <= 983040) return 'avc1.640033'; // 5.1: 1440p60, 4K30
  return 'avc1.640034'; // 5.2: 4K60
}
const ACODEC_SHARE = 12; // stereo Opus for the shared sound
const MAX_FRAME = 900 * 1024;

const SS = {
  // ---- sharing
  track: null, encoder: null, codecId: 0, w: 0, h: 0, fps: 15, count: 0, forceKey: false,
  q: null, hw: false, rateScale: 1, viewers: new Map(), decodable: null,
  timer: null, reader: null, lastTs: 0, sent: 0, sharing: false,
  // ---- watching
  watching: 0, decoder: null, decCodec: 0, waitKey: true, lastKeyReq: 0, ts: 0, frames: 0,
  // ---- shared sound
  audioTrack: null, aSource: null, aNode: null, aEncoder: null, aNo: 0, aSent: 0, withAudio: false, ownAudioFiltered: false,
  aDecoder: null, aFrames: 0, shareVolume: 1, shareMuted: false,
  onChange: null,

  async start(quality) {
    if (this.sharing) return;
    if (typeof VideoEncoder === 'undefined') throw new Error('Screen sharing needs a browser engine with WebCodecs (Edge/Chrome 94+).');
    // Ask for sound too. On Windows the share dialog offers "Also share
    // system audio" for screens and (recent Edge/Chrome) windows; tabs share
    // their own sound. restrictOwnAudio keeps SecureSpeak's own playback
    // (other people's voices) out of the shared sound.
    const sup = navigator.mediaDevices.getSupportedConstraints ? navigator.mediaDevices.getSupportedConstraints() : {};
    const audio = { echoCancellation: false, noiseSuppression: false, autoGainControl: false, channelCount: 2 };
    if (sup.restrictOwnAudio) audio.restrictOwnAudio = true;
    this.q = quality ? this.quality(quality) : this.quality();
    this.fps = this.q.fps;
    this.rateScale = 1;
    this.viewers = new Map();
    const video = { frameRate: { ideal: this.q.fps, max: this.q.fps } };
    if (this.q.height) { video.height = { max: this.q.height }; video.width = { max: Math.round(this.q.height * 32 / 9) }; }
    const stream = await navigator.mediaDevices.getDisplayMedia({
      video,
      audio, systemAudio: 'include', windowAudio: 'system', selfBrowserSurface: 'exclude',
    });
    this.track = stream.getVideoTracks()[0];
    this.audioTrack = stream.getAudioTracks()[0] || null;
    this.withAudio = !!this.audioTrack;
    this.ownAudioFiltered = !!(this.audioTrack && this.audioTrack.getSettings && this.audioTrack.getSettings().restrictOwnAudio);
    if (this.audioTrack) {
      try { await this.startAudio(); } catch (e) { console.warn('share audio', e); this.withAudio = false; }
    }
    try { this.track.contentHint = this.q.hint; } catch (e) { /* optional */ }
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
    this.stopAudio();
    act('share', { on: false });
    this.changed();
  },

  changed() { if (this.onChange) this.onChange(); },

  // the user's quality setting (S.share from the core)
  quality(choice) {
    const q = choice || (typeof S !== 'undefined' && S && S.share) || { preset: 'smooth' }; // S is app.js's state (not on window)
    const base = q.preset === 'custom'
      ? { fps: q.fps || 30, height: q.height, mbps: q.mbps || 8, hint: (q.fps || 30) >= 30 ? 'motion' : 'detail' }
      : SHARE_PRESETS[q.preset] || SHARE_PRESETS.smooth;
    return Object.assign({ preset: q.preset || 'smooth', software: !!q.software }, base);
  },

  // codecs every current viewer can decode
  allowed(id) {
    for (const codecs of this.viewers.values()) if (!codecs.includes(id)) return false;
    return true;
  },

  // A viewer (re)started watching or needs a keyframe; codecs = what it decodes.
  onViewer(from, codecs) {
    this.viewers.set(from, Array.isArray(codecs) && codecs.length ? codecs : LEGACY_CODECS);
    if (this.codecId && !this.allowed(this.codecId)) this.w = 0; // reconfigure with a codec everyone can watch
    this.forceKey = true;
  },

  info() {
    if (!this.sharing || !this.codecId) return '';
    return VNAME[this.codecId] + (this.hw ? ' · GPU' : ' · CPU') + ' · ' + this.fps + ' fps · ' + this.h + 'p';
  },

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
    const q = this.q || this.quality();
    const bitrate = Math.round((q.mbps ? q.mbps * 1e6 : Math.min(4e6, Math.max(8e5, w * h * 1.4))) * this.rateScale);
    // GPU first (unless switched off), then software
    const tries = [];
    if (!q.software) tries.push([13, 'prefer-hardware'], [11, 'prefer-hardware']);
    tries.push([11, 'no-preference'], [10, 'no-preference']);
    for (const [id, accel] of tries) {
      if (!this.allowed(id)) continue;
      const cfg = {
        codec: id === 13 ? avcCodec(w, h, this.fps) : VCODEC[id], width: w, height: h, framerate: this.fps,
        bitrate, bitrateMode: 'variable', latencyMode: 'realtime', hardwareAcceleration: accel,
      };
      if (id === 13) cfg.avc = { format: 'annexb' }; // parameter sets in-band: viewers can start at any keyframe
      let ok = false;
      try { ok = (await VideoEncoder.isConfigSupported(cfg)).supported; } catch (e) { ok = false; }
      if (!ok) continue;
      const enc = new VideoEncoder({
        output: (chunk) => this.onChunk(chunk, id),
        error: (e) => {
          console.warn('video encoder', e);
          this.encoder = null;
          if (accel === 'prefer-hardware') { this.q = Object.assign({}, this.q, { software: true }); } // GPU encoder failed: use software
        },
      });
      try { enc.configure(cfg); } catch (e) { continue; }
      this.encoder = enc; this.codecId = id; this.hw = accel === 'prefer-hardware';
      this.w = w; this.h = h; this.forceKey = true;
      this.changed();
      return;
    }
    throw new Error('No video encoder available');
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
      const key = this.forceKey || this.count % (this.fps * 20) === 0; // periodic keyframe every ~20 s
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
    if (chunk.byteLength > MAX_FRAME) {
      // too big to send (very high bitrate/resolution): lower the bitrate
      this.rateScale = Math.max(0.2, this.rateScale * 0.7);
      this.w = 0; this.forceKey = true;
      return;
    }
    const msg = new Uint8Array(3 + chunk.byteLength);
    msg[0] = 2;
    msg[1] = chunk.type === 'key' ? 1 : 0;
    msg[2] = codecId;
    chunk.copyTo(msg.subarray(3));
    if (AE.ws && AE.ws.readyState === 1) { AE.ws.send(msg); this.sent++; }
  },

  requestKeyframe(from, codecs) {
    if (from) this.onViewer(from, codecs);
    else this.forceKey = true;
  },

  // which codecs this browser can decode (sent when we start watching)
  async decodableCodecs() {
    if (this.decodable) return this.decodable;
    const out = [];
    if (typeof VideoDecoder !== 'undefined') {
      for (const id of [13, 11, 10]) {
        try { if ((await VideoDecoder.isConfigSupported({ codec: VCODEC[id] })).supported) out.push(id); } catch (e) { /* no */ }
      }
    }
    this.decodable = out.length ? out : LEGACY_CODECS;
    return this.decodable;
  },

  // ---- shared sound: capture -> stereo Opus 128 kbit/s -> core
  async startAudio() {
    const ctx = AE.ctx;
    if (!ctx || typeof AudioEncoder === 'undefined') throw new Error('no audio engine');
    const cfg = { codec: 'opus', sampleRate: 48000, numberOfChannels: 2, bitrate: 128000,
      opus: { frameDuration: 20000, complexity: 10, application: 'audio', signal: 'music' } };
    let sup = await AudioEncoder.isConfigSupported(cfg).catch(() => ({ supported: false }));
    if (!sup.supported) { delete cfg.opus.application; delete cfg.opus.signal; sup = await AudioEncoder.isConfigSupported(cfg); }
    if (!sup.supported) throw new Error('stereo Opus not supported');
    this.aEncoder = new AudioEncoder({
      output: (chunk) => this.onAudioChunk(chunk),
      error: (e) => { console.warn('share audio encoder', e); this.aEncoder = null; },
    });
    this.aEncoder.configure(cfg);
    this.aSource = ctx.createMediaStreamSource(new MediaStream([this.audioTrack]));
    this.aNode = new AudioWorkletNode(ctx, 'ss-capture', {
      processorOptions: { channels: 2 }, channelCount: 2, channelCountMode: 'explicit', numberOfOutputs: 1, outputChannelCount: [1],
    });
    this.aNode.port.onmessage = (e) => {
      if (!this.aEncoder || this.aEncoder.state !== 'configured' || this.aEncoder.encodeQueueSize > 10) return;
      const ad = new AudioData({ format: 'f32-planar', sampleRate: 48000, numberOfFrames: 960, numberOfChannels: 2, timestamp: (this.aNo++) * 20000, data: e.data });
      this.aEncoder.encode(ad);
      ad.close();
    };
    this.aSource.connect(this.aNode);
    this.aNode.connect(AE.sink); // keep the node pulled by the audio graph (silent)
  },

  stopAudio() {
    if (this.aNode) { this.aNode.port.onmessage = null; this.aNode.disconnect(); this.aNode = null; }
    if (this.aSource) { this.aSource.disconnect(); this.aSource = null; }
    if (this.aEncoder) { try { this.aEncoder.close(); } catch (e) { /* closed */ } this.aEncoder = null; }
    if (this.audioTrack) { this.audioTrack.stop(); this.audioTrack = null; }
    this.withAudio = false;
  },

  onAudioChunk(chunk) {
    const msg = new Uint8Array(3 + chunk.byteLength);
    msg[0] = 2; msg[1] = 0; msg[2] = ACODEC_SHARE;
    chunk.copyTo(msg.subarray(3));
    if (AE.ws && AE.ws.readyState === 1) { AE.ws.send(msg); this.aSent++; }
  },

  // ---- shared sound: viewer side
  onAudio(data) {
    if (typeof AudioDecoder === 'undefined' || !AE.player) return;
    if (!this.aDecoder || this.aDecoder.state === 'closed') {
      this.aDecoder = new AudioDecoder({
        output: (ad) => {
          const n = ad.numberOfFrames;
          const l = new Float32Array(n);
          let r = null;
          try {
            ad.copyTo(l, { planeIndex: 0, format: 'f32-planar' });
            if (ad.numberOfChannels > 1) { r = new Float32Array(n); ad.copyTo(r, { planeIndex: 1, format: 'f32-planar' }); }
          } catch (e) { ad.close(); return; }
          ad.close();
          this.aFrames++;
          AE.player.port.postMessage({ t: 'pcm', id: 'share', pcm: l, r }, r ? [l.buffer, r.buffer] : [l.buffer]);
        },
        error: (e) => { console.warn('share audio decoder', e); this.aDecoder = null; },
      });
      this.aDecoder.configure({ codec: 'opus', sampleRate: 48000, numberOfChannels: 2 });
      this.applyShareVolume();
    }
    if (this.aDecoder.decodeQueueSize > 20) return;
    try {
      this.aDecoder.decode(new EncodedAudioChunk({ type: 'key', timestamp: (this.aNo++) * 20000, data }));
    } catch (e) { this.aDecoder = null; }
  },

  applyShareVolume() {
    if (AE.player) AE.player.port.postMessage({ t: 'gain', id: 'share', g: this.shareMuted ? 0 : this.shareVolume });
  },

  stopShareAudio() {
    if (this.aDecoder) { try { this.aDecoder.close(); } catch (e) { /* closed */ } this.aDecoder = null; }
    if (AE.player) AE.player.port.postMessage({ t: 'reset', id: 'share' });
    this.aFrames = 0;
  },

  // ---- watching
  async watch(id) {
    if (this.watching && this.watching !== id) this.unwatch(true);
    this.watching = id;
    this.waitKey = true;
    this.frames = 0;
    this.changed();
    act('watch', { id, on: true, codecs: await this.decodableCodecs() });
  },

  unwatch(silent) {
    if (!this.watching) return;
    act('watch', { id: this.watching, on: false });
    this.watching = 0;
    if (this.decoder) { try { this.decoder.close(); } catch (e) { /* closed */ } this.decoder = null; }
    this.stopShareAudio();
    if (!silent) this.changed();
  },

  askKey() {
    const now = performance.now();
    if (now - this.lastKeyReq > 1000) { this.lastKeyReq = now; act('request_keyframe', { id: this.watching }); }
  },

  // [2][sender u32][flags][codec][payload]
  onFrame(b) {
    const sender = new DataView(b.buffer, b.byteOffset).getUint32(1);
    if (sender !== this.watching) return;
    const key = (b[5] & 1) === 1, codec = b[6];
    if (codec === ACODEC_SHARE) { this.onAudio(b.subarray(7)); return; }
    if (typeof VideoDecoder === 'undefined') return;
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

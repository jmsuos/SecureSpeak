'use strict';
// SecureSpeak audio engine.
//
// Capture: getUserMedia with the browser's WebRTC audio processing (noise
// suppression, echo cancellation, auto gain, optional voice isolation)
// -> AudioWorklet (48 kHz, 20 ms frames) -> voice activation / push-to-talk /
// whisper -> Opus via WebCodecs (IMA-ADPCM fallback) -> local WebSocket to the
// core, which encrypts, signs and sends the frames.
//
// Playback: decrypted frames from the core -> one Opus decoder per speaker ->
// AudioWorklet mixer with jitter buffering, per-user gain and a soft limiter.

const CODEC_ADPCM = 1, CODEC_OPUS = 2;
const FLAG_END = 1, FLAG_WHISPER = 2;
const MODE_QUICK_WHISPER = 255;

// ---- IMA-ADPCM (fallback codec, 24 kHz) ------------------------------------
const ADPCM_INDEX = [-1, -1, -1, -1, 2, 4, 6, 8, -1, -1, -1, -1, 2, 4, 6, 8];
const ADPCM_STEP = [7, 8, 9, 10, 11, 12, 13, 14, 16, 17, 19, 21, 23, 25, 28, 31, 34, 37, 41, 45, 50, 55, 60, 66, 73, 80, 88, 97, 107, 118, 130, 143, 157, 173, 190, 209, 230, 253, 279, 307, 337, 371, 408, 449, 494, 544, 598, 658, 724, 796, 876, 963, 1060, 1166, 1282, 1411, 1552, 1707, 1878, 2066, 2272, 2499, 2749, 3024, 3327, 3660, 4026, 4428, 4871, 5358, 5894, 6484, 7132, 7845, 8630, 9493, 10442, 11487, 12635, 13899, 15289, 16818, 18500, 20350, 22385, 24623, 27086, 29794, 32767];
function adpcmStep(st, code) {
  const s = ADPCM_STEP[st.index];
  let d = s >> 3;
  if (code & 4) d += s;
  if (code & 2) d += s >> 1;
  if (code & 1) d += s >> 2;
  st.pred += (code & 8) ? -d : d;
  st.pred = Math.max(-32768, Math.min(32767, st.pred));
  st.index = Math.max(0, Math.min(88, st.index + ADPCM_INDEX[code]));
}
class AdpcmEncoder {
  constructor() { this.st = { pred: 0, index: 0 }; }
  encode(pcm48) { // Float32 @48k (960) -> bytes @24k (480 samples)
    const n = pcm48.length >> 1;
    const out = new Uint8Array(3 + (n >> 1));
    const st = this.st;
    out[0] = st.pred & 0xff; out[1] = (st.pred >> 8) & 0xff; out[2] = st.index;
    let cur = 0;
    for (let i = 0; i < n; i++) {
      const smp = Math.max(-32768, Math.min(32767, Math.round((pcm48[2 * i] + pcm48[2 * i + 1]) * 16384)));
      const s = ADPCM_STEP[st.index];
      let diff = smp - st.pred, code = 0;
      if (diff < 0) { code = 8; diff = -diff; }
      if (diff >= s) { code |= 4; diff -= s; }
      if (diff >= s >> 1) { code |= 2; diff -= s >> 1; }
      if (diff >= s >> 2) code |= 1;
      adpcmStep(st, code);
      if (i % 2 === 0) cur = code; else out[3 + (i >> 1)] = cur | (code << 4);
    }
    return out;
  }
}
function adpcmDecode(b) { // -> Float32 @48k
  if (b.length < 4) return null;
  const st = { pred: (b[0] | (b[1] << 8)) << 16 >> 16, index: Math.min(88, b[2]) };
  const n = (b.length - 3) * 2;
  const out = new Float32Array(n * 2);
  let prev = st.pred / 32768;
  for (let i = 0; i < n; i++) {
    const byte = b[3 + (i >> 1)];
    adpcmStep(st, i % 2 ? byte >> 4 : byte & 15);
    const v = st.pred / 32768;
    out[2 * i] = (prev + v) / 2;
    out[2 * i + 1] = v;
    prev = v;
  }
  return out;
}

// ---- engine ---------------------------------------------------------------------
const AE = {
  ctx: null, ws: null, player: null, capture: null, stream: null, source: null, sink: null,
  encoder: null, adpcm: new AdpcmEncoder(), codec: 0, encMeta: [], frameNo: 0,
  settings: null, wantMic: false, micKey: '', micError: '', starting: null,
  pttKey: false, pttUI: false, whisperKey: -1, whisperUI: 0, pttUntil: 0, vaHang: 0,
  tx: false, txMode: 0, level: -90, outPeak: 0, loopback: false, connected: false, canTalk: false,
  decoders: new Map(), onLevel: null, onStatus: null, gains: new Map(),
  stats: { sent: 0, received: 0, decoded: 0 },

  async init(token) {
    this.token = token;
    try {
      this.ctx = new AudioContext({ latencyHint: 'interactive', sampleRate: 48000 });
    } catch (e) {
      this.ctx = new AudioContext({ latencyHint: 'interactive' });
    }
    await this.ctx.audioWorklet.addModule('worklet.js');
    this.player = new AudioWorkletNode(this.ctx, 'ss-player', { numberOfInputs: 0, outputChannelCount: [2] });
    this.player.port.onmessage = (e) => { this.outPeak = e.data.peak; };
    this.player.connect(this.ctx.destination);
    this.sink = this.ctx.createGain();
    this.sink.gain.value = 0;
    this.sink.connect(this.ctx.destination);
    this.connectWS();
    document.addEventListener('pointerdown', () => this.resume(), true);
    document.addEventListener('keydown', () => this.resume(), true);
    navigator.mediaDevices?.addEventListener?.('devicechange', () => this.onStatus && this.onStatus());
    setInterval(() => this.gcDecoders(), 10000);
  },

  resume() { if (this.ctx && this.ctx.state === 'suspended') this.ctx.resume().then(() => this.onStatus && this.onStatus()); },
  get suspended() { return this.ctx && this.ctx.state === 'suspended'; },

  connectWS() {
    const ws = new WebSocket('ws://' + location.host + '/audio?token=' + encodeURIComponent(this.token));
    ws.binaryType = 'arraybuffer';
    ws.onmessage = (e) => this.onNetFrame(new Uint8Array(e.data));
    ws.onclose = () => { this.ws = null; setTimeout(() => this.connectWS(), 1000); };
    ws.onopen = () => { this.ws = ws; };
  },

  // Apply settings and connection state from the core.
  async apply(S) {
    const a = S.audio;
    const old = this.settings;
    this.settings = a;
    this.connected = S.connected;
    this.canTalk = S.connected && !S.muted && !S.serverMuted;
    if (this.player) {
      this.player.port.postMessage({ t: 'master', g: a.outputVolume });
      for (const c of S.clients) {
        if (this.gains.get(c.id) !== c.volume) {
          this.gains.set(c.id, c.volume);
          this.player.port.postMessage({ t: 'gain', id: c.id, g: c.volume });
        }
      }
    }
    if (this.ctx && this.ctx.setSinkId && (!old || old.outputDevice !== a.outputDevice)) {
      this.ctx.setSinkId(a.outputDevice || '').catch((e) => console.warn('setSinkId', e));
    }
    if (old && old.bitrate !== a.bitrate && this.encoder) { // reconfigure lazily
      try { this.encoder.close(); } catch (e) { /* already closed */ }
      this.encoder = null;
    }
    if (!this.encoder) this.kickEncoder();
    await this.updateMic();
  },

  setMicWanted(on) { this.wantMic = on; this.updateMic(); },

  micConstraints() {
    const a = this.settings;
    const c = {
      channelCount: 1,
      noiseSuppression: a.noiseSuppression,
      echoCancellation: a.echoCancellation,
      autoGainControl: a.autoGain,
    };
    const sup = navigator.mediaDevices.getSupportedConstraints ? navigator.mediaDevices.getSupportedConstraints() : {};
    if (sup.voiceIsolation) c.voiceIsolation = a.voiceIsolation;
    if (a.inputDevice) c.deviceId = { exact: a.inputDevice };
    return c;
  },

  async updateMic() {
    if (!this.ctx || !this.settings) return;
    const want = this.wantMic || this.connected || this.loopback;
    const key = JSON.stringify(this.micConstraints());
    if (!want) return this.closeMic();
    if (this.stream && key === this.micKey) return;
    if (this.starting) return this.starting;
    this.starting = (async () => {
      this.closeMic();
      try {
        let stream;
        try {
          stream = await navigator.mediaDevices.getUserMedia({ audio: this.micConstraints() });
        } catch (e) {
          if (this.settings.inputDevice && e.name === 'OverconstrainedError') {
            const c = this.micConstraints(); delete c.deviceId;
            stream = await navigator.mediaDevices.getUserMedia({ audio: c });
          } else throw e;
        }
        this.stream = stream;
        this.micKey = key;
        this.source = this.ctx.createMediaStreamSource(stream);
        this.capture = new AudioWorkletNode(this.ctx, 'ss-capture', { numberOfOutputs: 1, outputChannelCount: [1] });
        this.capture.port.onmessage = (e) => this.onMicFrame(e.data);
        this.source.connect(this.capture);
        this.capture.connect(this.sink); // keep the node pulled by the graph
        this.micError = '';
      } catch (e) {
        this.micError = e.name === 'NotAllowedError' ? 'Microphone access was denied. Allow it via the lock/mic icon in the window and reopen settings.'
          : e.name === 'NotFoundError' ? 'No microphone found.' : (e.message || String(e));
        console.warn('microphone', e);
      }
      this.starting = null;
      this.onStatus && this.onStatus();
    })();
    return this.starting;
  },

  closeMic() {
    if (this.capture) { this.capture.port.onmessage = null; this.capture.disconnect(); this.capture = null; }
    if (this.source) { this.source.disconnect(); this.source = null; }
    if (this.stream) { this.stream.getTracks().forEach((t) => t.stop()); this.stream = null; }
    this.micKey = '';
  },

  micInfo() {
    const t = this.stream && this.stream.getAudioTracks()[0];
    return t ? { label: t.label, settings: t.getSettings ? t.getSettings() : {} } : null;
  },

  async ensureEncoder() {
    if (this.encoder && this.encoder.state === 'configured') return true;
    const cfg = {
      codec: 'opus', sampleRate: 48000, numberOfChannels: 1, bitrate: this.settings.bitrate * 1000,
      opus: { frameDuration: 20000, complexity: 10, useinbandfec: true, packetlossperc: 10 },
    };
    if (typeof AudioEncoder === 'undefined') { this.codec = CODEC_ADPCM; return false; }
    try {
      let sup = await AudioEncoder.isConfigSupported(cfg);
      if (!sup.supported) { delete cfg.opus; sup = await AudioEncoder.isConfigSupported(cfg); }
      if (!sup.supported) { this.codec = CODEC_ADPCM; return false; }
      this.encMeta = [];
      this.encoder = new AudioEncoder({
        output: (chunk) => this.onEncoded(chunk),
        error: (e) => { console.warn('opus encoder', e); this.encoder = null; },
      });
      this.encoder.configure(cfg);
      this.codec = CODEC_OPUS;
      return true;
    } catch (e) {
      console.warn('opus unavailable, using ADPCM', e);
      this.codec = CODEC_ADPCM;
      return false;
    }
  },

  // Called every 20 ms with 960 samples from the microphone.
  onMicFrame(pcm) {
    const a = this.settings;
    const g = a.inputGain;
    let sum = 0;
    for (let i = 0; i < pcm.length; i++) {
      let v = pcm[i] * g;
      if (v > 1) v = 1; else if (v < -1) v = -1;
      pcm[i] = v;
      sum += v * v;
    }
    this.level = Math.max(-90, 20 * Math.log10(Math.sqrt(sum / pcm.length) + 1e-9));

    let tx = false, mode = 0;
    const whisperMode = this.whisperKey >= 0 ? this.whisperKey + 1 : this.whisperUI;
    if (whisperMode) {
      tx = true; mode = whisperMode;
    } else if (a.mode === 'ptt') {
      const down = this.pttKey || this.pttUI;
      const now = performance.now();
      if (down) this.pttUntil = now + a.pttReleaseMs;
      tx = down || now < this.pttUntil;
    } else if (a.mode === 'continuous') {
      tx = true;
    } else {
      if (this.level > a.vaThreshold) this.vaHang = 20; // 400 ms hangover
      else if (this.vaHang > 0) this.vaHang--;
      tx = this.vaHang > 0;
    }
    if (!this.canTalk) tx = false;
    const send = tx || this.tx; // the frame after talking carries the end flag
    const meta = { send, end: !tx && this.tx, mode: tx ? mode : this.txMode };
    this.tx = tx;
    if (tx) this.txMode = mode;
    if (this.onLevel) this.onLevel(this.level, tx, mode);

    if (!send && !this.loopback) { if (this.codec !== CODEC_OPUS) return; }
    if (this.codec === CODEC_OPUS && this.encoder && this.encoder.state === 'configured') {
      // Encode every frame so the encoder state stays continuous; frames
      // that are not transmitted are dropped after encoding.
      this.encMeta.push(meta);
      if (this.encMeta.length > 20) this.encMeta.shift();
      const ad = new AudioData({ format: 'f32-planar', sampleRate: 48000, numberOfFrames: pcm.length, numberOfChannels: 1, timestamp: this.frameNo++ * 20000, data: pcm });
      this.encoder.encode(ad);
      ad.close();
    } else if (this.codec === CODEC_ADPCM) {
      if (send || this.loopback) this.emit(meta, CODEC_ADPCM, this.adpcm.encode(pcm));
    } else {
      this.kickEncoder();
    }
  },

  kickEncoder() {
    if (!this.encPending && this.settings) this.encPending = this.ensureEncoder().finally(() => { this.encPending = null; });
  },

  onEncoded(chunk) {
    const meta = this.encMeta.shift() || { send: false };
    if (!meta.send && !this.loopback) return;
    const b = new Uint8Array(chunk.byteLength);
    chunk.copyTo(b);
    this.emit(meta, CODEC_OPUS, b);
  },

  emit(meta, codec, payload) {
    if (this.loopback) this.decode(0, codec, payload, 0, this.frameNo);
    if (!meta.send || !this.ws || this.ws.readyState !== 1) return;
    const msg = new Uint8Array(4 + payload.length);
    msg[0] = 1; msg[1] = meta.mode; msg[2] = codec; msg[3] = meta.end ? FLAG_END : 0;
    msg.set(payload, 4);
    this.ws.send(msg);
    this.stats.sent++;
  },

  // [1][sender u32][flags][codec][seq u32][payload]
  onNetFrame(b) {
    if (b[0] === 2) { if (window.SS) window.SS.onFrame(b); return; } // screen share
    if (b.length < 12 || b[0] !== 1) return;
    const dv = new DataView(b.buffer, b.byteOffset);
    const sender = dv.getUint32(1), flags = b[5], codec = b[6], seq = dv.getUint32(7);
    this.stats.received++;
    this.decode(sender, codec, b.subarray(11), flags, seq);
  },

  decode(sender, codec, payload, flags, seq) {
    if (codec === CODEC_ADPCM) {
      const pcm = adpcmDecode(payload);
      if (pcm) this.play(sender, pcm);
      return;
    }
    if (codec !== CODEC_OPUS || typeof AudioDecoder === 'undefined') return;
    let d = this.decoders.get(sender);
    if (!d || d.dec.state === 'closed') {
      const dec = new AudioDecoder({
        output: (ad) => this.onDecoded(sender, ad),
        error: (e) => { console.warn('opus decoder', e); this.decoders.delete(sender); },
      });
      dec.configure({ codec: 'opus', sampleRate: 48000, numberOfChannels: 1 });
      d = { dec, last: 0 };
      this.decoders.set(sender, d);
    }
    d.last = performance.now();
    if (d.dec.decodeQueueSize > 25) return; // decoder overloaded: drop
    try {
      d.dec.decode(new EncodedAudioChunk({ type: 'key', timestamp: seq * 20000, data: payload }));
    } catch (e) {
      this.decoders.delete(sender);
    }
  },

  onDecoded(sender, ad) {
    const n = ad.numberOfFrames;
    const pcm = new Float32Array(n);
    try {
      ad.copyTo(pcm, { planeIndex: 0, format: 'f32-planar' });
    } catch (e) {
      try {
        if (ad.numberOfChannels === 1 && ad.format.startsWith('f32')) ad.copyTo(pcm, { planeIndex: 0 });
        else { ad.close(); return; }
      } catch (e2) { ad.close(); return; }
    }
    ad.close();
    this.stats.decoded++;
    this.play(sender, pcm);
  },

  play(sender, pcm) {
    if (this.player) this.player.port.postMessage({ t: 'pcm', id: sender, pcm }, [pcm.buffer]);
  },

  gcDecoders() {
    const now = performance.now();
    for (const [id, d] of this.decoders) {
      if (now - d.last > 30000) { try { d.dec.close(); } catch (e) { /* closed */ } this.decoders.delete(id); }
    }
  },

  setLoopback(on) {
    this.loopback = on;
    if (on) this.kickEncoder();
    this.updateMic();
  },

  // short synthesized notification sounds
  sound(name) {
    const seqs = {
      join: [660, 880], leave: [880, 660], move: [740, 740], msg: [1046], poke: [988, 0, 988, 0, 988],
      connect: [523, 659, 784], disconnect: [784, 659, 523], error: [330, 0, 330], mute: [600], unmute: [900],
      file: [880, 1175],
    };
    const f = seqs[name];
    if (!f || !this.ctx || !this.settings || !this.settings.sounds) return;
    const t0 = this.ctx.currentTime + 0.01;
    const vol = 0.18 * this.settings.soundVolume;
    f.forEach((hz, i) => {
      if (!hz) return;
      const o = this.ctx.createOscillator(), g = this.ctx.createGain();
      o.type = 'sine'; o.frequency.value = hz;
      const s = t0 + i * 0.07;
      g.gain.setValueAtTime(0, s);
      g.gain.linearRampToValueAtTime(vol, s + 0.008);
      g.gain.setValueAtTime(vol, s + 0.055);
      g.gain.linearRampToValueAtTime(0, s + 0.068);
      o.connect(g).connect(this.ctx.destination);
      o.start(s); o.stop(s + 0.07);
    });
  },

  codecName() {
    if (this.codec === CODEC_OPUS) return 'Opus ' + this.settings.bitrate + ' kbit/s, 48 kHz';
    if (this.codec === CODEC_ADPCM) return 'ADPCM 24 kHz (Opus not available in this browser)';
    return '…';
  },
};
window.AE = AE;

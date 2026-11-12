'use strict';
// SecureSpeak audio worklets. Everything on the wire is 48 kHz; these
// processors resample to and from the AudioContext rate when they differ.

const RATE = 48000;
const FRAME = 960; // 20 ms

// Capture: posts 20 ms frames as planar Float32Arrays (channel 0, then 1…).
// processorOptions.channels: 1 (microphone) or 2 (screen share sound).
class Capture extends AudioWorkletProcessor {
  constructor(opts) {
    super();
    this.ch = (opts && opts.processorOptions && opts.processorOptions.channels) || 1;
    this.buf = new Float32Array(FRAME * this.ch);
    this.n = 0;
    this.step = sampleRate / RATE; // input samples per output sample
    this.pos = 0;
    this.prev = new Float32Array(this.ch);
    this.cur = new Float32Array(this.ch);
    this.out = new Float32Array(this.ch);
  }
  push(vals) {
    for (let c = 0; c < this.ch; c++) this.buf[c * FRAME + this.n] = vals[c];
    if (++this.n === FRAME) {
      this.port.postMessage(this.buf, [this.buf.buffer]);
      this.buf = new Float32Array(FRAME * this.ch);
      this.n = 0;
    }
  }
  process(inputs) {
    const inp = inputs[0];
    if (!inp || !inp[0]) return true;
    const len = inp[0].length;
    const tmp = this.cur;
    for (let i = 0; i < len; i++) {
      for (let c = 0; c < this.ch; c++) tmp[c] = (inp[c] || inp[0])[i]; // mono input -> both channels
      if (sampleRate === RATE) { this.push(tmp); continue; }
      while (this.pos < 1) {
        const out = this.out;
        for (let c = 0; c < this.ch; c++) out[c] = this.prev[c] + (tmp[c] - this.prev[c]) * this.pos;
        this.push(out);
        this.pos += this.step;
      }
      this.pos -= 1;
      this.prev.set(tmp);
    }
    return true;
  }
}

const PREFILL = RATE * 0.06;   // start playing a stream after 60 ms buffered
const MAXBUF = RATE * 0.25;    // above 250 ms, drop audio to catch up
const TRIM_TO = RATE * 0.08;

// Player: mixes any number of mono (voices) and stereo (screen share sound)
// streams with per-stream gain, jitter buffering and a soft limiter.
class Player extends AudioWorkletProcessor {
  constructor() {
    super();
    this.q = new Map();
    this.gains = new Map();
    this.master = 1;
    this.step = RATE / sampleRate;
    this.peak = 0;
    this.meterAt = 0;
    this.R = new Float32Array(128);
    this.port.onmessage = (e) => this.msg(e.data);
  }
  stream(id) {
    let s = this.q.get(id);
    if (!s) {
      s = { chunks: [], len: 0, off: 0, playing: false, al: 0, bl: 0, ar: 0, br: 0, frac: 1, idle: 0, l: 0, r: 0 };
      this.q.set(id, s);
    }
    return s;
  }
  msg(m) {
    if (m.t === 'pcm') {
      const s = this.stream(m.id);
      s.chunks.push({ l: m.pcm, r: m.r || null });
      s.len += m.pcm.length;
      s.idle = 0;
      while (s.len > MAXBUF && s.chunks.length > 1) { // latency control
        const c = s.chunks.shift();
        s.len -= c.l.length - s.off;
        s.off = 0;
        if (s.len <= TRIM_TO) break;
      }
    } else if (m.t === 'gain') {
      this.gains.set(m.id, m.g);
    } else if (m.t === 'master') {
      this.master = m.g;
    } else if (m.t === 'reset') {
      this.q.delete(m.id);
    }
  }
  // reads the next sample pair into s.l / s.r
  pop(s) {
    while (s.chunks.length) {
      const c = s.chunks[0];
      if (s.off < c.l.length) {
        s.l = c.l[s.off];
        s.r = c.r ? c.r[s.off] : s.l;
        s.off++;
        s.len--;
        return;
      }
      s.chunks.shift();
      s.off = 0;
    }
    s.l = 0; s.r = 0;
  }
  process(inputs, outputs) {
    const out = outputs[0];
    const L = out[0];
    const n = L.length;
    if (this.R.length !== n) this.R = new Float32Array(n);
    const R = this.R;
    L.fill(0); R.fill(0);
    for (const [id, s] of this.q) {
      if (!s.playing) {
        if (s.len >= PREFILL) s.playing = true;
        else { if (++s.idle > 3000) this.q.delete(id); continue; }
      }
      const g = this.gains.has(id) ? this.gains.get(id) : 1;
      if (this.step === 1) {
        for (let i = 0; i < n && s.len > 0; i++) { this.pop(s); L[i] += s.l * g; R[i] += s.r * g; }
      } else {
        for (let i = 0; i < n; i++) {
          while (s.frac >= 1) { s.al = s.bl; s.ar = s.br; this.pop(s); s.bl = s.l; s.br = s.r; s.frac -= 1; }
          L[i] += (s.al + (s.bl - s.al) * s.frac) * g;
          R[i] += (s.ar + (s.br - s.ar) * s.frac) * g;
          s.frac += this.step;
        }
      }
      if (s.len <= 0) { s.playing = false; s.len = 0; } // drained: re-buffer next time
    }
    let peak = this.peak;
    for (let i = 0; i < n; i++) {
      let l = L[i] * this.master, r = R[i] * this.master;
      if (l > 0.75 || l < -0.75) l = Math.tanh(l); // soft limiter
      if (r > 0.75 || r < -0.75) r = Math.tanh(r);
      L[i] = l; R[i] = r;
      const a = Math.max(l < 0 ? -l : l, r < 0 ? -r : r);
      if (a > peak) peak = a;
    }
    if (out[1]) out[1].set(R);
    for (let c = 2; c < out.length; c++) out[c].set(L);
    this.peak = peak;
    this.meterAt += n;
    if (this.meterAt >= sampleRate / 10) {
      this.port.postMessage({ peak: this.peak });
      this.peak = 0;
      this.meterAt = 0;
    }
    return true;
  }
}

registerProcessor('ss-capture', Capture);
registerProcessor('ss-player', Player);

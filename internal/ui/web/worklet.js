'use strict';
// SecureSpeak audio worklets. Everything on the wire is 48 kHz mono; these
// processors resample to and from the AudioContext rate when they differ.

const RATE = 48000;
const FRAME = 960; // 20 ms

class Capture extends AudioWorkletProcessor {
  constructor() {
    super();
    this.buf = new Float32Array(FRAME);
    this.n = 0;
    this.step = sampleRate / RATE; // input samples per output sample
    this.pos = 0;
    this.prev = 0;
  }
  push(v) {
    this.buf[this.n++] = v;
    if (this.n === FRAME) {
      this.port.postMessage(this.buf, [this.buf.buffer]);
      this.buf = new Float32Array(FRAME);
      this.n = 0;
    }
  }
  process(inputs, outputs) {
    const ch = inputs[0] && inputs[0][0];
    if (ch) {
      if (sampleRate === RATE) {
        for (let i = 0; i < ch.length; i++) this.push(ch[i]);
      } else {
        for (let i = 0; i < ch.length; i++) {
          const cur = ch[i];
          while (this.pos < 1) { this.push(this.prev + (cur - this.prev) * this.pos); this.pos += this.step; }
          this.pos -= 1;
          this.prev = cur;
        }
      }
    }
    return true;
  }
}

const PREFILL = RATE * 0.06;   // start playing a speaker after 60 ms buffered
const MAXBUF = RATE * 0.25;    // above 250 ms, drop audio to catch up
const TRIM_TO = RATE * 0.08;

class Player extends AudioWorkletProcessor {
  constructor() {
    super();
    this.q = new Map();
    this.gains = new Map();
    this.master = 1;
    this.step = RATE / sampleRate;
    this.peak = 0;
    this.meterAt = 0;
    this.port.onmessage = (e) => this.msg(e.data);
  }
  stream(id) {
    let s = this.q.get(id);
    if (!s) {
      s = { chunks: [], len: 0, off: 0, playing: false, a: 0, b: 0, frac: 1, idle: 0 };
      this.q.set(id, s);
    }
    return s;
  }
  msg(m) {
    if (m.t === 'pcm') {
      const s = this.stream(m.id);
      s.chunks.push(m.pcm);
      s.len += m.pcm.length;
      s.idle = 0;
      while (s.len > MAXBUF && s.chunks.length > 1) { // latency control
        const c = s.chunks.shift();
        s.len -= c.length - s.off;
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
  pop(s) {
    while (s.chunks.length) {
      const c = s.chunks[0];
      if (s.off < c.length) { s.len--; return c[s.off++]; }
      s.chunks.shift();
      s.off = 0;
    }
    return 0;
  }
  process(inputs, outputs) {
    const out = outputs[0];
    const L = out[0];
    const n = L.length;
    L.fill(0);
    for (const [id, s] of this.q) {
      if (!s.playing) {
        if (s.len >= PREFILL) s.playing = true;
        else { if (++s.idle > 3000) this.q.delete(id); continue; }
      }
      const g = this.gains.has(id) ? this.gains.get(id) : 1;
      if (this.step === 1) {
        for (let i = 0; i < n && s.len > 0; i++) L[i] += this.pop(s) * g;
      } else {
        for (let i = 0; i < n; i++) {
          while (s.frac >= 1) { s.a = s.b; s.b = this.pop(s); s.frac -= 1; }
          L[i] += (s.a + (s.b - s.a) * s.frac) * g;
          s.frac += this.step;
        }
      }
      if (s.len <= 0) { s.playing = false; s.len = 0; } // drained: re-buffer next time
    }
    let peak = this.peak;
    for (let i = 0; i < n; i++) {
      let v = L[i] * this.master;
      if (v > 0.75 || v < -0.75) v = Math.tanh(v); // soft limiter
      L[i] = v;
      const a = v < 0 ? -v : v;
      if (a > peak) peak = a;
    }
    for (let c = 1; c < out.length; c++) out[c].set(L);
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

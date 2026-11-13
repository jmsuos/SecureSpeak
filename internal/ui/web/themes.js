'use strict';
// Color themes, based on the SecureSpeak logo: navy shield, blue glow,
// silver rim, red bandana and warm brown tones. Each theme sets the CSS
// variables from app.css. Loaded before app.js so the saved theme is
// applied before the first paint (no flash of the default colors).

const DARK_STATUS = { talk: '#39d98a', ok: '#39d98a', warn: '#f5b84a', danger: '#f2555a', whisper: '#b47cff',
  okText: '#9fe8c4', warnText: '#f3d197', dangerText: '#f6a5a8', shade: '5, 7, 11' };
const LIGHT_STATUS = { talk: '#14995c', ok: '#14995c', warn: '#b7740f', danger: '#cf3340', whisper: '#7d43d6',
  okText: '#0f6b41', warnText: '#7a4d06', dangerText: '#a3222e', shade: '20, 28, 45' };

const THEMES = {
  navy: { name: 'SecureSpeak Navy', desc: 'Deep navy with the blue glow of the shield', scheme: 'dark',
    bg: '#0a1120', panel: '#0f1a2e', panel2: '#14223b', panel3: '#1b2d4d', line: '#22365c',
    text: '#e7eef9', dim: '#93a4c3', faint: '#5f7194', accent: '#2f80ff', accent2: '#6aa8ff' },
  midnight: { name: 'Midnight Shield', desc: 'Almost black, with a cyan glow', scheme: 'dark',
    bg: '#05070d', panel: '#0a0f1a', panel2: '#0f1624', panel3: '#162033', line: '#1c2a42',
    text: '#e3ecf7', dim: '#8696b3', faint: '#56647e', accent: '#1fb6ff', accent2: '#6fd3ff' },
  bandana: { name: 'Bandana Red', desc: 'Navy uniform with the red bandana', scheme: 'dark',
    bg: '#0b1220', panel: '#101a2d', panel2: '#15213a', panel3: '#1d2c4a', line: '#253658',
    text: '#eef2f9', dim: '#97a5bf', faint: '#62718d', accent: '#e23a4e', accent2: '#ff6b7a' },
  pinup: { name: 'Pin-up Retro', desc: 'Warm and dark, cherry red and cream', scheme: 'dark',
    bg: '#161012', panel: '#1e1619', panel2: '#261c20', panel3: '#31242a', line: '#3b2b32',
    text: '#f4e9dc', dim: '#b9a596', faint: '#7d6a60', accent: '#e8484f', accent2: '#ff7a74' },
  espresso: { name: 'Espresso', desc: 'Brown tones with an amber accent', scheme: 'dark',
    bg: '#120d0a', panel: '#1a130f', panel2: '#221913', panel3: '#2c2119', line: '#382a1f',
    text: '#f1e6da', dim: '#b5a28f', faint: '#7b6a5a', accent: '#d9883b', accent2: '#f0a75c' },
  ocean: { name: 'Deep Ocean', desc: 'Dark teal, calm and cool', scheme: 'dark',
    bg: '#06141a', panel: '#0a1c24', panel2: '#0e242e', panel3: '#142f3b', line: '#1a3a48',
    text: '#e2f1f4', dim: '#8fb0b8', faint: '#5a7a83', accent: '#19b9b0', accent2: '#4fd8cf' },
  steel: { name: 'Steel', desc: 'Neutral graphite with silver-blue', scheme: 'dark',
    bg: '#111317', panel: '#171a1f', panel2: '#1d2127', panel3: '#252a32', line: '#2e343d',
    text: '#e6e9ee', dim: '#9aa2ae', faint: '#646c78', accent: '#8fb2e8', accent2: '#b3cdf3', onAccent: '#0b1220' },
  graphite: { name: 'Graphite Classic', desc: 'The original SecureSpeak look', scheme: 'dark',
    bg: '#0d1016', panel: '#141922', panel2: '#1a202b', panel3: '#212937', line: '#262f3d',
    text: '#e3e8f1', dim: '#8b95a7', faint: '#5b6577', accent: '#5b7cfa', accent2: '#7b96ff' },
  silver: { name: 'Silver Rim', desc: 'Light silver with navy and blue', scheme: 'light',
    bg: '#e8ecf3', panel: '#f5f7fb', panel2: '#ffffff', panel3: '#e2e8f1', line: '#d2d9e5',
    text: '#14213b', dim: '#55627a', faint: '#8691a6', accent: '#1f5fd6', accent2: '#1f5fd6' },
  daylight: { name: 'Pin-up Daylight', desc: 'Light cream with cherry red', scheme: 'light',
    bg: '#f3ece3', panel: '#faf6f1', panel2: '#ffffff', panel3: '#eee3d6', line: '#e0d2c1',
    text: '#2a1d18', dim: '#6e5a4f', faint: '#a08c80', accent: '#c8323f', accent2: '#b82a37' },
};
const DEFAULT_THEME = 'navy';

function hexRGB(h) {
  const n = parseInt(h.slice(1), 16);
  return ((n >> 16) & 255) + ', ' + ((n >> 8) & 255) + ', ' + (n & 255);
}

function applyTheme(id) {
  const t = THEMES[id] || THEMES[DEFAULT_THEME];
  const st = t.scheme === 'light' ? LIGHT_STATUS : DARK_STATUS;
  const r = document.documentElement.style;
  const set = { '--bg': t.bg, '--panel': t.panel, '--panel-2': t.panel2, '--panel-3': t.panel3, '--line': t.line,
    '--text': t.text, '--dim': t.dim, '--faint': t.faint, '--accent': t.accent, '--accent-2': t.accent2,
    '--accent-rgb': hexRGB(t.accent), '--on-accent': t.onAccent || '#fff',
    '--talk': st.talk, '--ok': st.ok, '--warn': st.warn, '--danger': st.danger, '--whisper': st.whisper,
    '--ok-text': st.okText, '--warn-text': st.warnText, '--danger-text': st.dangerText, '--shade-rgb': st.shade };
  for (const [k, v] of Object.entries(set)) r.setProperty(k, v);
  r.colorScheme = t.scheme;
  document.documentElement.dataset.theme = THEMES[id] ? id : DEFAULT_THEME;
  let meta = document.querySelector('meta[name="theme-color"]');
  if (!meta) { meta = document.createElement('meta'); meta.name = 'theme-color'; document.head.append(meta); }
  meta.content = t.panel; // title bar color of the app window
  try { localStorage.setItem('ss-theme', id); } catch (e) { /* storage unavailable */ }
}

// before the first paint: the last theme used in this window
try { applyTheme(localStorage.getItem('ss-theme') || DEFAULT_THEME); } catch (e) { applyTheme(DEFAULT_THEME); }
window.THEMES = THEMES;
window.applyTheme = applyTheme;

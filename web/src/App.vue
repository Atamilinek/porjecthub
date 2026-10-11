<template>
  <main class="page-shell">
    <section class="welcome-panel" aria-labelledby="project-hub-title">
      <div class="brand-mark" aria-hidden="true">PH</div>
      <p class="eyebrow">INTERNAL PROJECT PORTAL</p>
      <h1 id="project-hub-title">Project Hub</h1>
      <p class="intro">你的项目入口与运行状态，将在安全连接建立后显示在这里。</p>
      <div class="connection-state" role="status" aria-live="polite">
        <span class="state-dot" aria-hidden="true"></span>
        <span>尚未连接到 API</span>
      </div>
    </section>
    <footer>Project Hub · Self-hosted</footer>
  </main>
</template>

<style>
:root {
  font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
  color: #e8eef8;
  background: #0b1321;
  font-synthesis: none;
  text-rendering: optimizeLegibility;
  -webkit-font-smoothing: antialiased;
  -moz-osx-font-smoothing: grayscale;
}

* { box-sizing: border-box; }

body {
  min-width: 320px;
  min-height: 100vh;
  margin: 0;
  background:
    radial-gradient(ellipse at 50% 35%, rgba(35, 76, 130, 0.25), transparent 55%),
    #0b1321;
}

.page-shell {
  display: grid;
  min-height: 100vh;
  grid-template-rows: 1fr auto;
  padding: clamp(24px, 6vw, 80px);
}

.welcome-panel {
  width: min(100%, 640px);
  align-self: center;
  justify-self: center;
  padding: clamp(28px, 6vw, 64px);
  border: 1px solid rgba(163, 190, 229, 0.16);
  border-radius: 24px;
  background: rgba(17, 29, 48, 0.82);
  box-shadow: 0 28px 90px rgba(0, 0, 0, 0.28);
}

.brand-mark {
  display: grid;
  width: 48px;
  height: 48px;
  place-items: center;
  border-radius: 15px;
  background: linear-gradient(145deg, #80b7ff, #477de0);
  color: #091426;
  font-size: 15px;
  font-weight: 800;
  letter-spacing: -0.06em;
}

.eyebrow {
  margin: 34px 0 12px;
  color: #83a9dc;
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.16em;
}

h1 {
  margin: 0;
  font-size: clamp(36px, 7vw, 56px);
  letter-spacing: -0.055em;
  line-height: 1.05;
}

.intro {
  max-width: 440px;
  margin: 20px 0 32px;
  color: #a6b5ca;
  font-size: 16px;
  line-height: 1.75;
}

.connection-state {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  padding: 10px 14px;
  border: 1px solid rgba(163, 190, 229, 0.14);
  border-radius: 999px;
  background: rgba(8, 16, 29, 0.58);
  color: #c2cee0;
  font-size: 13px;
}

.state-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #e4a64d;
  box-shadow: 0 0 12px rgba(228, 166, 77, 0.45);
}

footer {
  justify-self: center;
  padding-top: 32px;
  color: #63738b;
  font-size: 12px;
}

@media (max-width: 480px) {
  .page-shell { padding: 20px; }
  .welcome-panel { padding: 28px 24px; border-radius: 20px; }
  .eyebrow { margin-top: 28px; }
  .intro { font-size: 15px; }
}
</style>

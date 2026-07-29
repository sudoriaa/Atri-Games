const GAME_DURATION = 45;

const games = {
  "neon-relay": {
    title: "霓光中继",
    eyebrow: "ARCADE SIGNAL RUN · DEMO BUILD 1.3",
    intro: "穿过折叠航线，把脉冲送往城市中继站。连续收集会让信号倍率持续上升。",
    goal: "移动信号艇收集发光节点；避开紫色失真区域，在 45 秒内建立最长的中继链。",
    note: "Canvas 动作试玩 · 本地运行 · 不需要平台 SDK",
    accent: "#ccff33",
    accent2: "#7d66ff",
    background: "#171229",
    speed: 390,
    drag: 4.8,
    hazards: 4,
  },
  "circuit-bloom": {
    title: "电路花园",
    eyebrow: "GARDEN LOGIC · DEMO BUILD 0.9",
    intro: "唤醒休眠节点，让能量沿着机械花园重新流动。每一次连通都会开出一朵光。",
    goal: "依次触碰能量芽点完成回路；别碰到漂移的断路脉冲，连续连通会获得额外分数。",
    note: "React 主题概念试玩 · 静态独立页面 · 可离线运行",
    accent: "#ff8265",
    accent2: "#38d6b2",
    background: "#102a2a",
    speed: 320,
    drag: 5.8,
    hazards: 3,
  },
  "echo-vault": {
    title: "回声档案",
    eyebrow: "MEMORY ARCHIVE · DEMO BUILD 1.1",
    intro: "档案会逐渐淡去。沿着回声留下的短暂线索，赶在它们消失前保存记忆碎片。",
    goal: "捕捉会明灭的档案碎片；蓝色静默区会打断调查链，观察节奏比速度更重要。",
    note: "联网叙事作品的本地概念试玩 · 正式版可连接自有后端",
    accent: "#90e8ff",
    accent2: "#bb5574",
    background: "#101827",
    speed: 300,
    drag: 6.5,
    hazards: 3,
  },
  "paper-orbit": {
    title: "纸上轨道",
    eyebrow: "INK & GRAVITY · DEMO BUILD 1.0",
    intro: "让探测器借助纸上星体的引力滑行。动作越连贯，有限墨水就能延伸得越远。",
    goal: "控制纸片探测器收集金色轨道点；它的惯性更强，提前转向才能避开墨团。",
    note: "Phaser 概念试玩 · 纯浏览器静态制品",
    accent: "#f3c85b",
    accent2: "#e95c45",
    background: "#19253a",
    speed: 430,
    drag: 3.2,
    hazards: 4,
  },
  "pixel-forge": {
    title: "像素铸造局",
    eyebrow: "TINY FACTORY SHIFT · DEMO BUILD 0.8",
    intro: "订单正在堆积。带着自动助手穿过工坊，收集正确材料并远离失控的废料箱。",
    goal: "收集彩色材料方块完成订单；连续交付提高工坊声望，碰撞会损失当前连击。",
    note: "Godot Web 主题概念试玩 · 正式作品可使用独立服务",
    accent: "#ff9f43",
    accent2: "#49c6bc",
    background: "#241a19",
    speed: 340,
    drag: 5.1,
    hazards: 5,
  },
  "memory-tide": {
    title: "潮汐记忆",
    eyebrow: "ISLAND RHYTHM · DEMO BUILD 0.7",
    intro: "潮水会抹去道路，只有灯火还记得港湾。跟随短暂亮起的路径，把光送回远处。",
    goal: "收集岛屿上的引路灯；潮汐暗流会推开你，保持连续移动才能守住记忆。",
    note: "Unity Web 主题概念试玩 · 轻量本地演示制品",
    accent: "#ffbf69",
    accent2: "#6e93ff",
    background: "#0e2735",
    speed: 315,
    drag: 5.6,
    hazards: 4,
  },
};

const slug = new URLSearchParams(location.search).get("game") || "neon-relay";
const config = games[slug] || games["neon-relay"];
const root = document.documentElement;
root.style.setProperty("--accent", config.accent);
root.style.setProperty("--accent-2", config.accent2);
document.title = `${config.title} · Atri Games 独立试玩`;
document.querySelector("#game-title").textContent = config.title;
document.querySelector("#game-eyebrow").textContent = config.eyebrow;
document.querySelector("#game-intro").textContent = config.intro;
document.querySelector("#game-goal").textContent = config.goal;
document.querySelector("#game-note").textContent = config.note;
document.querySelector("#screen-title").textContent = `进入「${config.title}」`;
document.querySelector("#back-link").href = `/games/${slug}`;

const canvas = document.querySelector("#game-canvas");
const context = canvas.getContext("2d");
const startScreen = document.querySelector("#start-screen");
const resultScreen = document.querySelector("#result-screen");
const scoreElement = document.querySelector("#score");
const comboElement = document.querySelector("#combo");
const timeElement = document.querySelector("#time");
const finalScoreElement = document.querySelector("#final-score");
const bestComboElement = document.querySelector("#best-combo");

const keys = new Set();
let width = 1000;
let height = 540;
let dpr = 1;
let running = false;
let lastTime = 0;
let timeLeft = GAME_DURATION;
let score = 0;
let combo = 1;
let bestCombo = 1;
let flash = 0;
let animationFrame = 0;
let pointer = null;
let platformMenuOpen = false;
let nodes = [];
let hazards = [];
let particles = [];
let trails = [];

const player = { x: 0, y: 0, vx: 0, vy: 0, radius: 12 };

function hexToRgb(hex) {
  const value = Number.parseInt(hex.slice(1), 16);
  return { r: value >> 16, g: (value >> 8) & 255, b: value & 255 };
}

function rgba(hex, alpha) {
  const { r, g, b } = hexToRgb(hex);
  return `rgba(${r}, ${g}, ${b}, ${alpha})`;
}

function random(min, max) {
  return min + Math.random() * (max - min);
}

function resize() {
  const bounds = canvas.getBoundingClientRect();
  dpr = Math.min(window.devicePixelRatio || 1, 2);
  width = Math.max(320, bounds.width);
  height = Math.max(420, bounds.height);
  canvas.width = Math.round(width * dpr);
  canvas.height = Math.round(height * dpr);
  context.setTransform(dpr, 0, 0, dpr, 0, 0);
  if (!running && player.x === 0) {
    player.x = width / 2;
    player.y = height / 2;
  }
}

function makeNode() {
  const margin = Math.min(width, height) * 0.11;
  return {
    x: random(margin, width - margin),
    y: random(margin, height - margin),
    radius: random(8, 13),
    phase: random(0, Math.PI * 2),
  };
}

function makeHazard(index) {
  const radius = random(20, 42);
  const angle = random(0, Math.PI * 2);
  const speed = random(28, 56) + index * 2;
  return {
    x: random(radius, width - radius),
    y: random(radius, height - radius),
    vx: Math.cos(angle) * speed,
    vy: Math.sin(angle) * speed,
    radius,
    phase: random(0, Math.PI * 2),
  };
}

function resetGame() {
  score = 0;
  combo = 1;
  bestCombo = 1;
  timeLeft = GAME_DURATION;
  flash = 0;
  player.x = width / 2;
  player.y = height / 2;
  player.vx = 0;
  player.vy = 0;
  nodes = Array.from({ length: 3 }, makeNode);
  hazards = Array.from({ length: config.hazards }, (_, index) => makeHazard(index));
  particles = [];
  trails = [];
  updateHud();
}

function startGame() {
  cancelAnimationFrame(animationFrame);
  resetGame();
  startScreen.classList.add("screen--hidden");
  resultScreen.classList.add("screen--hidden");
  running = true;
  lastTime = performance.now();
  animationFrame = requestAnimationFrame(frame);
}

function finishGame() {
  running = false;
  finalScoreElement.textContent = score.toLocaleString("zh-CN");
  bestComboElement.textContent = `×${bestCombo}`;
  resultScreen.classList.remove("screen--hidden");
}

function updateHud() {
  scoreElement.textContent = String(score).padStart(4, "0");
  comboElement.textContent = `×${combo}`;
  timeElement.textContent = timeLeft.toFixed(1);
}

function burst(x, y, color, amount = 16) {
  for (let index = 0; index < amount; index += 1) {
    const angle = random(0, Math.PI * 2);
    const speed = random(35, 150);
    particles.push({
      x,
      y,
      vx: Math.cos(angle) * speed,
      vy: Math.sin(angle) * speed,
      life: random(.4, .9),
      size: random(2, 6),
      color,
    });
  }
}

function collectNode(index) {
  const node = nodes[index];
  score += 100 * combo;
  combo = Math.min(combo + 1, 12);
  bestCombo = Math.max(bestCombo, combo);
  burst(node.x, node.y, config.accent, 20);
  nodes[index] = makeNode();
  updateHud();
}

function hitHazard(hazard) {
  const angle = Math.atan2(player.y - hazard.y, player.x - hazard.x);
  player.vx += Math.cos(angle) * 260;
  player.vy += Math.sin(angle) * 260;
  score = Math.max(0, score - 75);
  combo = 1;
  flash = .3;
  burst(player.x, player.y, config.accent2, 12);
  updateHud();
}

function update(delta) {
  let dx = 0;
  let dy = 0;
  if (keys.has("arrowleft") || keys.has("a")) dx -= 1;
  if (keys.has("arrowright") || keys.has("d")) dx += 1;
  if (keys.has("arrowup") || keys.has("w")) dy -= 1;
  if (keys.has("arrowdown") || keys.has("s")) dy += 1;

  if (pointer) {
    const pointerDx = pointer.x - player.x;
    const pointerDy = pointer.y - player.y;
    const distance = Math.hypot(pointerDx, pointerDy);
    if (distance > 8) {
      dx += pointerDx / distance;
      dy += pointerDy / distance;
    }
  }

  const length = Math.hypot(dx, dy) || 1;
  if (dx || dy) {
    player.vx += (dx / length) * config.speed * delta;
    player.vy += (dy / length) * config.speed * delta;
  }
  const damping = Math.exp(-config.drag * delta);
  player.vx *= damping;
  player.vy *= damping;
  player.x += player.vx * delta;
  player.y += player.vy * delta;

  if (player.x < player.radius) { player.x = player.radius; player.vx = Math.abs(player.vx) * .6; }
  if (player.x > width - player.radius) { player.x = width - player.radius; player.vx = -Math.abs(player.vx) * .6; }
  if (player.y < player.radius) { player.y = player.radius; player.vy = Math.abs(player.vy) * .6; }
  if (player.y > height - player.radius) { player.y = height - player.radius; player.vy = -Math.abs(player.vy) * .6; }

  trails.unshift({ x: player.x, y: player.y, life: 1 });
  if (trails.length > 32) trails.pop();
  trails.forEach((trail) => { trail.life -= delta * 2.3; });

  nodes.forEach((node, index) => {
    node.phase += delta * 2.2;
    if (Math.hypot(player.x - node.x, player.y - node.y) < player.radius + node.radius + 5) collectNode(index);
  });

  hazards.forEach((hazard) => {
    hazard.phase += delta;
    hazard.x += hazard.vx * delta;
    hazard.y += hazard.vy * delta;
    if (hazard.x < hazard.radius || hazard.x > width - hazard.radius) hazard.vx *= -1;
    if (hazard.y < hazard.radius || hazard.y > height - hazard.radius) hazard.vy *= -1;
    if (Math.hypot(player.x - hazard.x, player.y - hazard.y) < player.radius + hazard.radius && hazard.cooldown !== true) {
      hazard.cooldown = true;
      hitHazard(hazard);
      setTimeout(() => { hazard.cooldown = false; }, 650);
    }
  });

  particles.forEach((particle) => {
    particle.x += particle.vx * delta;
    particle.y += particle.vy * delta;
    particle.vx *= Math.exp(-2.8 * delta);
    particle.vy *= Math.exp(-2.8 * delta);
    particle.life -= delta;
  });
  particles = particles.filter((particle) => particle.life > 0);
  flash = Math.max(0, flash - delta);
  timeLeft = Math.max(0, timeLeft - delta);
  if (timeLeft <= 0) finishGame();
  updateHud();
}

function drawBackground(now) {
  const gradient = context.createRadialGradient(width * .65, height * .32, 10, width * .55, height * .45, Math.max(width, height));
  gradient.addColorStop(0, rgba(config.accent2, .25));
  gradient.addColorStop(.48, config.background);
  gradient.addColorStop(1, "#09080c");
  context.fillStyle = gradient;
  context.fillRect(0, 0, width, height);

  context.lineWidth = 1;
  for (let row = 0; row < 8; row += 1) {
    context.beginPath();
    for (let x = -20; x <= width + 20; x += 18) {
      const y = (height / 7) * row + Math.sin(x * .012 + now * .0004 + row) * (12 + row * 2);
      if (x === -20) context.moveTo(x, y);
      else context.lineTo(x, y);
    }
    context.strokeStyle = rgba(config.accent, .06 + row * .006);
    context.stroke();
  }

  context.strokeStyle = "rgba(255,255,255,.045)";
  context.setLineDash([2, 12]);
  for (let x = 0; x < width; x += 90) {
    context.beginPath();
    context.moveTo(x, 0);
    context.lineTo(x, height);
    context.stroke();
  }
  for (let y = 0; y < height; y += 90) {
    context.beginPath();
    context.moveTo(0, y);
    context.lineTo(width, y);
    context.stroke();
  }
  context.setLineDash([]);
}

function draw(now) {
  context.clearRect(0, 0, width, height);
  drawBackground(now);

  hazards.forEach((hazard) => {
    const pulse = Math.sin(hazard.phase * 2) * 3;
    context.beginPath();
    context.arc(hazard.x, hazard.y, hazard.radius + pulse + 10, 0, Math.PI * 2);
    context.fillStyle = rgba(config.accent2, .08);
    context.fill();
    context.beginPath();
    context.arc(hazard.x, hazard.y, hazard.radius + pulse, 0, Math.PI * 2);
    context.fillStyle = rgba(config.accent2, .22);
    context.fill();
    context.strokeStyle = rgba(config.accent2, .7);
    context.lineWidth = 1.4;
    context.stroke();
    context.beginPath();
    context.moveTo(hazard.x - hazard.radius * .55, hazard.y - hazard.radius * .55);
    context.lineTo(hazard.x + hazard.radius * .55, hazard.y + hazard.radius * .55);
    context.moveTo(hazard.x + hazard.radius * .55, hazard.y - hazard.radius * .55);
    context.lineTo(hazard.x - hazard.radius * .55, hazard.y + hazard.radius * .55);
    context.strokeStyle = rgba(config.accent2, .45);
    context.stroke();
  });

  nodes.forEach((node) => {
    const pulse = 1 + Math.sin(node.phase) * .18;
    const halo = context.createRadialGradient(node.x, node.y, 0, node.x, node.y, node.radius * 5);
    halo.addColorStop(0, rgba(config.accent, .48));
    halo.addColorStop(1, rgba(config.accent, 0));
    context.fillStyle = halo;
    context.beginPath();
    context.arc(node.x, node.y, node.radius * 5, 0, Math.PI * 2);
    context.fill();
    context.fillStyle = config.accent;
    context.beginPath();
    context.arc(node.x, node.y, node.radius * pulse, 0, Math.PI * 2);
    context.fill();
    context.fillStyle = "#ffffff";
    context.beginPath();
    context.arc(node.x - node.radius * .22, node.y - node.radius * .22, node.radius * .26, 0, Math.PI * 2);
    context.fill();
  });

  trails.forEach((trail, index) => {
    if (trail.life <= 0) return;
    context.beginPath();
    context.arc(trail.x, trail.y, Math.max(1, player.radius * (1 - index / 38)), 0, Math.PI * 2);
    context.fillStyle = rgba(config.accent, Math.max(0, trail.life) * .16);
    context.fill();
  });

  particles.forEach((particle) => {
    context.beginPath();
    context.arc(particle.x, particle.y, particle.size * Math.min(1, particle.life * 2), 0, Math.PI * 2);
    context.fillStyle = rgba(particle.color, Math.min(1, particle.life * 1.8));
    context.fill();
  });

  const playerGlow = context.createRadialGradient(player.x, player.y, 0, player.x, player.y, 44);
  playerGlow.addColorStop(0, rgba(config.accent, .65));
  playerGlow.addColorStop(1, rgba(config.accent, 0));
  context.fillStyle = playerGlow;
  context.beginPath();
  context.arc(player.x, player.y, 44, 0, Math.PI * 2);
  context.fill();
  context.fillStyle = "#fff";
  context.beginPath();
  context.arc(player.x, player.y, player.radius, 0, Math.PI * 2);
  context.fill();
  context.strokeStyle = config.accent;
  context.lineWidth = 4;
  context.stroke();

  if (flash > 0) {
    context.fillStyle = rgba(config.accent2, flash * .55);
    context.fillRect(0, 0, width, height);
  }
}

function frame(now) {
  if (!running) return;
  if (platformMenuOpen) {
    lastTime = now;
    animationFrame = requestAnimationFrame(frame);
    return;
  }
  const delta = Math.min((now - lastTime) / 1000, .033);
  lastTime = now;
  update(delta);
  draw(now);
  if (running) animationFrame = requestAnimationFrame(frame);
}

function pointerPosition(event) {
  const bounds = canvas.getBoundingClientRect();
  return { x: event.clientX - bounds.left, y: event.clientY - bounds.top };
}

window.addEventListener("resize", resize);
window.addEventListener("keydown", (event) => {
  const key = event.key.toLowerCase();
  if (["arrowleft", "arrowright", "arrowup", "arrowdown", "w", "a", "s", "d"].includes(key)) {
    keys.add(key);
    event.preventDefault();
  }
});
window.addEventListener("keyup", (event) => keys.delete(event.key.toLowerCase()));
window.addEventListener("atri-platform-menu", (event) => {
  platformMenuOpen = event.detail?.open === true;
  keys.clear();
  pointer = null;
  if (!platformMenuOpen && running) lastTime = performance.now();
});
canvas.addEventListener("pointerdown", (event) => {
  pointer = pointerPosition(event);
  canvas.setPointerCapture(event.pointerId);
});
canvas.addEventListener("pointermove", (event) => {
  if (pointer) pointer = pointerPosition(event);
});
canvas.addEventListener("pointerup", (event) => {
  pointer = null;
  if (canvas.hasPointerCapture(event.pointerId)) canvas.releasePointerCapture(event.pointerId);
});
canvas.addEventListener("pointercancel", () => { pointer = null; });
document.addEventListener("visibilitychange", () => {
  if (document.hidden) keys.clear();
});
document.querySelector("#start-button").addEventListener("click", startGame);
document.querySelector("#restart-button").addEventListener("click", startGame);

resize();
resetGame();
draw(performance.now());

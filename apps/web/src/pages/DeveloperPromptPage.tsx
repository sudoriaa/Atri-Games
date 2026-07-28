import {
  ArrowRight,
  Bot,
  Check,
  Clipboard,
  Download,
  ExternalLink,
  FileArchive,
  Settings2,
  ShieldAlert,
  Sparkles,
  WandSparkles,
} from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import {
  buildAiGameIntegrationPrompt,
  defaultAiGameIntegrationPromptConfig,
  normalizeAtriGameId,
  type AiGameIntegrationPromptConfig,
  type AtriRuntimeChoice,
} from "../content/ai-game-integration-prompt";

type CopyState = "idle" | "copied" | "selected";

const runtimeChoices: Array<{ value: AtriRuntimeChoice; label: string; description: string }> = [
  { value: "auto", label: "让 AI 判断", description: "先审计项目，再选择静态包或外部 URL。" },
  { value: "static", label: "静态 .atri", description: "构建产物由 Atri 托管，可接平台能力。" },
  { value: "external", label: "外部 URL", description: "保留自己的后端、数据库和部署。" },
];

function legacyCopy(text: string) {
  const field = document.createElement("textarea");
  field.value = text;
  field.setAttribute("readonly", "");
  field.style.position = "fixed";
  field.style.inset = "0 auto auto -9999px";
  field.style.opacity = "0";
  document.body.appendChild(field);
  field.focus();
  field.select();
  const copied = document.execCommand("copy");
  field.remove();
  return copied;
}

export function DeveloperPromptPage() {
  const [config, setConfig] = useState<AiGameIntegrationPromptConfig>(defaultAiGameIntegrationPromptConfig);
  const [copyState, setCopyState] = useState<CopyState>("idle");
  const resetTimer = useRef<number | null>(null);
  const promptRef = useRef<HTMLPreElement | null>(null);
  const prompt = useMemo(() => buildAiGameIntegrationPrompt(config), [config]);
  const external = config.runtime === "external";

  useEffect(
    () => () => {
      if (resetTimer.current !== null) window.clearTimeout(resetTimer.current);
    },
    [],
  );

  function update<K extends keyof AiGameIntegrationPromptConfig>(key: K, value: AiGameIntegrationPromptConfig[K]) {
    setConfig((current) => ({ ...current, [key]: value }));
    setCopyState("idle");
  }

  function setRuntime(runtime: AtriRuntimeChoice) {
    update("runtime", runtime);
  }

  function announceCopy(state: CopyState) {
    setCopyState(state);
    if (resetTimer.current !== null) window.clearTimeout(resetTimer.current);
    resetTimer.current = window.setTimeout(() => setCopyState("idle"), 2400);
  }

  async function copyPrompt() {
    try {
      if (navigator.clipboard && window.isSecureContext) {
        await navigator.clipboard.writeText(prompt);
        announceCopy("copied");
        return;
      }
      if (legacyCopy(prompt)) {
        announceCopy("copied");
        return;
      }
    } catch {
      // The selectable prompt below is the final fallback.
    }

    const selection = window.getSelection();
    if (promptRef.current && selection) {
      const range = document.createRange();
      range.selectNodeContents(promptRef.current);
      selection.removeAllRanges();
      selection.addRange(range);
    }
    announceCopy("selected");
  }

  function downloadPrompt() {
    const blob = new Blob([prompt], { type: "text/markdown;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = `${normalizeAtriGameId(config.gameId)}-atri-ai-prompt.md`;
    document.body.appendChild(link);
    link.click();
    link.remove();
    URL.revokeObjectURL(url);
  }

  return (
    <div className="developer-prompt-page">
      <section className="developer-prompt-hero">
        <div className="developer-prompt-hero__copy">
          <p className="kicker"><Sparkles size={15} /> AI-NATIVE GAME ONBOARDING</p>
          <h1>把项目交给 AI，<br />拿回一个可导入的 <em>.atri</em>。</h1>
          <p>
            填写项目线索，生成一份面向编程 AI 的完整工程任务书。它会要求 AI 实际审计、改造、构建、校验和打包，而不是只给你一段教程。
          </p>
          <div className="developer-prompt-hero__actions">
            <a className="button button--large" href="#prompt-builder">生成我的提示词 <ArrowRight size={18} /></a>
            <a
              className="text-link"
              href="https://github.com/sudoriaa/Atri-Games/blob/main/docs/GAME_INTEGRATION.md"
              target="_blank"
              rel="noreferrer"
            >
              阅读人工接入文档 <ExternalLink size={14} />
            </a>
          </div>
        </div>
        <div className="developer-prompt-hero__visual" aria-hidden="true">
          <div className="prompt-orbit prompt-orbit--one" />
          <div className="prompt-orbit prompt-orbit--two" />
          <div className="prompt-machine">
            <WandSparkles />
            <strong>PROJECT</strong>
            <span>↓</span>
            <strong>.ATRI</strong>
          </div>
          <span className="prompt-sticker prompt-sticker--top">ANY AI · ANY STACK</span>
          <span className="prompt-sticker prompt-sticker--bottom">BUILD → VALIDATE → PACK</span>
        </div>
      </section>

      <section className="developer-compliance" aria-labelledby="developer-compliance-title">
        <div className="developer-compliance__heading">
          <ShieldAlert aria-hidden="true" />
          <div>
            <p className="kicker">CONTENT RULES / BEFORE YOU BUILD</p>
            <h2 id="developer-compliance-title">先确认游戏内容符合平台收录规范。</h2>
          </div>
        </div>
        <div className="developer-compliance__body">
          <p>
            Atri Games 面向中国大陆用户运营。提交、导入和外链登记前，请确认游戏玩法、文案、角色、画面、音频、广告位及跳转链接均符合中国法律法规与社会公德。
          </p>
          <ul>
            <li>禁止政治敏感、煽动对立、歪曲历史或损害国家统一与社会稳定的内容。</li>
            <li>禁止色情低俗、涉黄擦边、赌博博彩、代币变现、诈骗、违法犯罪与危险行为诱导。</li>
            <li>禁止违规引流、私域拉新、外部交易、虚假宣传、恶意弹窗及与游戏无关的推广链接。</li>
            <li>禁止仇恨歧视、侮辱骚扰、侵犯他人隐私、著作权或人格权，以及其他违背公序良俗的设计。</li>
          </ul>
          <p className="developer-compliance__note">
            这条规范同样适用于 AI 生成的素材、剧情、提示词与更新内容。下方生成的接入提示词已包含提交前自检要求；发现风险时请先调整项目内容，再打包导入。
          </p>
        </div>
      </section>

      <section className="developer-prompt-steps" aria-label="使用步骤">
        <article><span>01</span><Bot /><h2>打开项目</h2><p>让编程 AI 获得游戏仓库的读写和终端能力。</p></article>
        <article><span>02</span><Settings2 /><h2>填写线索</h2><p>选择托管方式、登录、SQLite 存档和匹配需求。</p></article>
        <article><span>03</span><Clipboard /><h2>复制提示词</h2><p>整段发送给 AI，让它持续执行到校验和打包完成。</p></article>
        <article><span>04</span><FileArchive /><h2>导入 .atri</h2><p>把 AI 交付的文件上传到管理后台，选择状态并导入。</p></article>
      </section>

      <section className="developer-prompt-builder" id="prompt-builder">
        <header className="developer-prompt-builder__heading">
          <div>
            <p className="kicker">PROMPT GENERATOR / SCHEMA V2</p>
            <h2>生成项目专属接入任务书</h2>
          </div>
          <p>不知道的字段保留“让 AI 自动识别”即可。提示词会要求它以仓库事实为准，并解释所有调整。</p>
        </header>

        <div className="developer-prompt-workspace">
          <form className="developer-prompt-config" onSubmit={(event) => event.preventDefault()}>
            <div className="developer-prompt-config__title">
              <Settings2 size={20} />
              <div><strong>项目线索</strong><small>不会上传或保存，仅在本机浏览器生成文本。</small></div>
            </div>

            <div className="developer-prompt-fields">
              <label>
                <span>游戏名称</span>
                <input value={config.projectName} onChange={(event) => update("projectName", event.target.value)} />
              </label>
              <label>
                <span>永久游戏 ID</span>
                <input
                  value={config.gameId}
                  onChange={(event) => update("gameId", event.target.value)}
                  autoCapitalize="none"
                  spellCheck={false}
                />
                <small>会规范为：{normalizeAtriGameId(config.gameId)}</small>
              </label>
              <label>
                <span>作者 / 团队</span>
                <input value={config.authorName} onChange={(event) => update("authorName", event.target.value)} />
              </label>
              <label>
                <span>技术栈</span>
                <input value={config.techStack} onChange={(event) => update("techStack", event.target.value)} />
              </label>
              <label>
                <span>构建命令</span>
                <input
                  value={config.buildCommand}
                  onChange={(event) => update("buildCommand", event.target.value)}
                  spellCheck={false}
                />
              </label>
              <label>
                <span>构建输出目录</span>
                <input
                  value={config.buildOutput}
                  onChange={(event) => update("buildOutput", event.target.value)}
                  spellCheck={false}
                />
              </label>
            </div>

            <fieldset className="developer-prompt-runtime">
              <legend>运行方式</legend>
              <div className="developer-prompt-choice-grid">
                {runtimeChoices.map((choice) => (
                  <label className={config.runtime === choice.value ? "prompt-choice is-active" : "prompt-choice"} key={choice.value}>
                    <input
                      type="radio"
                      name="runtime"
                      value={choice.value}
                      checked={config.runtime === choice.value}
                      onChange={() => setRuntime(choice.value)}
                    />
                    <strong>{choice.label}</strong>
                    <span>{choice.description}</span>
                  </label>
                ))}
              </div>
            </fieldset>

            {(config.runtime === "auto" || external) && (
              <label className="developer-prompt-wide-field">
                <span>外部游戏 HTTPS 地址 {external && <b>必填</b>}</span>
                <input
                  type="url"
                  value={config.externalUrl}
                  onChange={(event) => update("externalUrl", event.target.value)}
                  placeholder="https://games.example.com/my-game/"
                  spellCheck={false}
                />
              </label>
            )}

            <fieldset className="developer-prompt-capabilities" disabled={external}>
              <legend>平台能力</legend>
              {external && <p className="developer-prompt-field-note">外部 URL 游戏使用自己的后端，提示词会自动关闭 Atri 内置能力。</p>}
              <div className="developer-prompt-fields">
                <label>
                  <span>玩家身份</span>
                  <select
                    value={config.matchmaking ? "required" : config.identity}
                    onChange={(event) => update("identity", event.target.value as AiGameIntegrationPromptConfig["identity"])}
                    disabled={config.matchmaking}
                  >
                    <option value="none">不使用平台身份</option>
                    <option value="optional">可选登录，匿名可玩</option>
                    <option value="required">必须登录</option>
                  </select>
                  {config.matchmaking && <small>开启匹配后自动要求登录。</small>}
                </label>
                <label>
                  <span>玩家数据</span>
                  <select
                    value={config.storage}
                    onChange={(event) => update("storage", event.target.value as AiGameIntegrationPromptConfig["storage"])}
                  >
                    <option value="default">默认 SQLite 兜底</option>
                    <option value="sqlite">明确启用 SQLite 云存档</option>
                    <option value="none">完全关闭平台存储</option>
                  </select>
                </label>
              </div>
              <label className="developer-prompt-toggle">
                <input
                  type="checkbox"
                  checked={config.matchmaking}
                  onChange={(event) => {
                    const enabled = event.target.checked;
                    setConfig((current) => ({
                      ...current,
                      matchmaking: enabled,
                      identity: enabled ? "required" : current.identity,
                    }));
                    setCopyState("idle");
                  }}
                />
                <span aria-hidden="true" />
                <div><strong>接入 Atri 内置匹配队列</strong><small>负责按 game + mode + region 配对；实时对局传输仍由游戏负责。</small></div>
              </label>
            </fieldset>

            <button
              className="developer-prompt-reset"
              type="button"
              onClick={() => {
                setConfig(defaultAiGameIntegrationPromptConfig);
                setCopyState("idle");
              }}
            >
              恢复默认设置
            </button>
          </form>

          <div className="developer-prompt-output" id="ai-prompt">
            <div className="developer-prompt-output__toolbar">
              <div>
                <span className="developer-prompt-output__status"><i /> READY FOR AI</span>
                <small>Schema v2 · {prompt.length.toLocaleString("zh-CN")} 字符</small>
              </div>
              <div className="developer-prompt-output__actions">
                <button className="button button--ghost button--small" type="button" onClick={downloadPrompt}>
                  <Download size={15} /> 下载 .md
                </button>
                <button className="button button--small" type="button" onClick={copyPrompt}>
                  {copyState === "copied" ? <Check size={16} /> : <Clipboard size={16} />}
                  {copyState === "copied" ? "已复制完整提示词" : "复制完整提示词"}
                </button>
              </div>
            </div>
            <p className="developer-prompt-copy-notice" role="status" aria-live="polite">
              {copyState === "copied" && "已复制，可以直接粘贴给你的编程 AI。"}
              {copyState === "selected" && "浏览器已选中全文，请按 Ctrl+C 或 ⌘C 复制。"}
              {copyState === "idle" && "提示词会随左侧选项实时更新。"}
            </p>
            <pre ref={promptRef} tabIndex={0}><code>{prompt}</code></pre>
          </div>
        </div>
      </section>

      <section className="developer-prompt-assurance">
        <header>
          <p className="kicker">WHAT THE AI MUST DELIVER</p>
          <h2>它不只创建一个 JSON。</h2>
          <p>完整提示词把工程改造、平台边界和验收标准都写进任务，减少“看似适配、实际打不开”的交付。</p>
        </header>
        <div>
          <article><Check /><h3>真实构建</h3><p>识别框架、修复相对资源和子路径路由，把生产产物完整放入 game/。</p></article>
          <article><Check /><h3>能力对齐</h3><p>按选择接入玩家票据、SQLite 存档和匹配，并处理匿名、断网、配额与超时。</p></article>
          <article><Check /><h3>可验证包</h3><p>执行 validate、pack 和归档检查，最后报告文件路径、大小、哈希和测试证据。</p></article>
        </div>
        <a
          className="button button--light"
          href="https://github.com/sudoriaa/Atri-Games/blob/main/schemas/game-manifest.schema.json"
          target="_blank"
          rel="noreferrer"
        >
          查看 Manifest Schema <ExternalLink size={16} />
        </a>
      </section>
    </div>
  );
}

import { ArrowRight, Asterisk, Cpu, Gamepad2, Sparkles } from "lucide-react";
import { Link } from "react-router-dom";
import { GameCard } from "../components/GameCard";
import { ErrorState, LoadingState } from "../components/PageState";
import { useAuth } from "../lib/auth";
import { useAsync } from "../lib/use-async";

export function HomePage() {
  const { api } = useAuth();
  const featured = useAsync(() => api.games({ featured: true, pageSize: 3 }), [api]);
  const latest = useAsync(() => api.games({ pageSize: 6 }), [api]);
  const categories = useAsync(() => api.categories(), [api]);

  return (
    <>
      <section className="hero">
        <div className="hero__grain" />
        <div className="hero__copy">
          <p className="kicker"><Sparkles size={15} /> HUMAN IMAGINATION × AI</p>
          <h1>这里收藏的，<br />是<span>还没被定义</span>的游戏。</h1>
          <p className="hero__lead">来自独立创作者的网页游戏展馆。无需安装，打开浏览器，下一秒就进入另一个世界。</p>
          <div className="hero__actions">
            <Link className="button" to="/discover">开始探索 <ArrowRight size={17} /></Link>
            <Link className="text-link" to="/developers">我是开发者 <ArrowRight size={15} /></Link>
          </div>
          <div className="hero__stats">
            <div><strong>06</strong><span>首发作品</span></div>
            <div><strong>05</strong><span>技术引擎</span></div>
            <div><strong>01</strong><span>共同入口</span></div>
          </div>
        </div>
        <div className="hero__visual" aria-hidden="true">
          <div className="orbit orbit--one" />
          <div className="orbit orbit--two" />
          <div className="hero-disc"><span>PLAY</span><Gamepad2 /><small>IN BROWSER</small></div>
          <div className="floating-note floating-note--top"><Asterisk /> CURATED WEEKLY</div>
          <div className="floating-note floating-note--bottom"><Cpu /> ANY STACK</div>
        </div>
      </section>

      <section className="section section--featured">
        <div className="section-heading">
          <div><p className="kicker">EDITOR'S FIELD NOTES</p><h2>本周值得打开的三个世界</h2></div>
          <Link className="text-link" to="/discover?featured=true">全部精选 <ArrowRight size={15} /></Link>
        </div>
        {featured.loading && <LoadingState />}
        {featured.error && <ErrorState message={featured.error} retry={featured.reload} />}
        {featured.data && <div className="game-grid game-grid--featured">{featured.data.items.map((game, index) => <GameCard key={game.id} game={game} index={index} />)}</div>}
      </section>

      <section className="manifesto">
        <p className="manifesto__index">NO. 001 / OUR BELIEF</p>
        <blockquote><span>“</span>工具可以生成代码，<br />但<span className="marker">游戏的灵魂</span>仍来自那个<br />想让别人感到惊喜的人。</blockquote>
        <p>我们不规定创作工具，也不把游戏关在同一种容器里。每个作品都有自己的技术与边界，而 Atri 负责让它们被看见。</p>
      </section>

      <section className="section">
        <div className="section-heading"><div><p className="kicker">BROWSE BY MOOD</p><h2>今天想进入哪一种状态？</h2></div></div>
        <div className="category-rail">
          {categories.data?.map((category, index) => (
            <Link key={category.id} to={`/discover?category=${category.id}`} className={`category-ticket category-ticket--${(index % 4) + 1}`}>
              <small>{String(index + 1).padStart(2, "0")}</small><h3>{category.name}</h3><p>{category.description}</p><span>{category.gameCount ?? 0} 个世界 <ArrowRight size={16} /></span>
            </Link>
          ))}
        </div>
      </section>

      <section className="section section--latest">
        <div className="section-heading">
          <div><p className="kicker">RECENTLY CATALOGUED</p><h2>刚刚抵达展馆</h2></div>
          <Link className="button button--ghost button--small" to="/discover">浏览全部 <ArrowRight size={15} /></Link>
        </div>
        {latest.loading && <LoadingState />}
        {latest.error && <ErrorState message={latest.error} retry={latest.reload} />}
        {latest.data && <div className="game-grid">{latest.data.items.map((game, index) => <GameCard key={game.id} game={game} index={index} />)}</div>}
      </section>

      <section className="developer-callout">
        <div><p className="kicker">FOR AI GAME MAKERS</p><h2>把项目交给 AI。<br />拿回可导入的 .atri。</h2></div>
        <div>
          <p>任何技术栈、任何引擎、静态或联网。填写几项项目线索，复制完整提示词，让编程 AI 自动改造、构建、校验并打包。</p>
          <Link className="button button--light" to="/developers">生成 AI 接入提示词 <ArrowRight size={17} /></Link>
        </div>
      </section>
    </>
  );
}

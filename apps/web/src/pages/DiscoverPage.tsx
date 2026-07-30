import { ArrowDownUp, Grid2X2, List as ListIcon, Search, SlidersHorizontal, X } from "lucide-react";
import { useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { GameCard } from "../components/GameCard";
import { ErrorState, LoadingState } from "../components/PageState";
import { useAuth } from "../lib/auth";
import { useAsync } from "../lib/use-async";

type CatalogSort = "newest" | "recommended" | "likes" | "plays";
type CatalogView = "card" | "list";

const catalogViewStorageKey = "atri_catalog_view";

function readCatalogView(): CatalogView {
  try {
    return localStorage.getItem(catalogViewStorageKey) === "list" ? "list" : "card";
  } catch {
    return "card";
  }
}

export function DiscoverPage() {
  const { api } = useAuth();
  const [params, setParams] = useSearchParams();
  const [draftQuery, setDraftQuery] = useState(params.get("q") ?? "");
  const [view, setView] = useState<CatalogView>(readCatalogView);
  const query = params.get("q") ?? "";
  const category = params.get("category") ?? "";
  const featured = params.get("featured") ?? "";
  const requestedSort = params.get("sort");
  const sort: CatalogSort = requestedSort === "recommended" || requestedSort === "likes" || requestedSort === "plays" ? requestedSort : "newest";
  const page = Number(params.get("page") ?? 1);
  const request = useMemo(() => ({ query, category, featured: featured || undefined, sort, page, pageSize: 12 }), [category, featured, page, query, sort]);
  const games = useAsync(() => api.games(request), [api, request]);
  const categories = useAsync(() => api.categories(), [api]);

  const update = (key: string, value: string) => {
    const next = new URLSearchParams(params);
    if (value) next.set(key, value); else next.delete(key);
    if (key !== "page") next.delete("page");
    setParams(next);
  };

  const submit = (event: React.FormEvent) => { event.preventDefault(); update("q", draftQuery.trim()); };
  const clearFilters = () => {
    setDraftQuery("");
    const next = new URLSearchParams(params);
    ["q", "category", "featured", "page"].forEach((key) => next.delete(key));
    setParams(next);
  };
  const selectView = (next: CatalogView) => {
    setView(next);
    try {
      localStorage.setItem(catalogViewStorageKey, next);
    } catch {
      // Storage can be unavailable in privacy-restricted browser contexts.
    }
  };

  return (
    <div className="page-wrap discover-page">
      <header className="page-intro"><p className="kicker">THE OPEN CATALOGUE</p><h1>发现下一个<br /><em>停不下来的</em>小游戏。</h1><p>按兴趣浏览，或者输入一个模糊的念头。</p></header>
      <div className="catalog-toolbar">
        <form className="catalog-search" onSubmit={submit}><Search /><input value={draftQuery} onChange={(event) => setDraftQuery(event.target.value)} placeholder="比如：轻松、太空、像素……" aria-label="搜索目录" />{draftQuery && <button type="button" className="icon-button" onClick={() => { setDraftQuery(""); update("q", ""); }} aria-label="清空搜索"><X size={17} /></button>}</form>
        <div className="catalog-toolbar__controls">
          <div className="result-count"><Grid2X2 size={17} /><strong>{games.data?.total ?? "—"}</strong> 个游戏</div>
          <label className="catalog-sort">
            <ArrowDownUp size={15} aria-hidden="true" />
            <span>排序</span>
            <select
              aria-label="游戏排序方式"
              value={sort}
              onChange={(event) => update("sort", event.target.value === "newest" ? "" : event.target.value)}
            >
              <option value="newest">最新发布</option>
              <option value="recommended">综合推荐</option>
              <option value="likes">点赞最多</option>
              <option value="plays">游玩最多</option>
            </select>
          </label>
          <div className="catalog-view-switch" role="group" aria-label="目录显示方式">
            <button type="button" className={view === "card" ? "is-active" : ""} aria-pressed={view === "card"} onClick={() => selectView("card")} title="卡片模式">
              <Grid2X2 size={15} /><span>卡片</span>
            </button>
            <button type="button" className={view === "list" ? "is-active" : ""} aria-pressed={view === "list"} onClick={() => selectView("list")} title="列表模式">
              <ListIcon size={16} /><span>列表</span>
            </button>
          </div>
        </div>
      </div>
      <div className="catalog-layout">
        <aside className="filter-panel">
          <div className="filter-title"><SlidersHorizontal size={17} /> 筛选目录</div>
          <button className={!category ? "filter-option active" : "filter-option"} onClick={() => update("category", "")}>全部类型 <span>{games.data?.total ?? 0}</span></button>
          {categories.data?.map((item) => <button key={item.id} className={category === item.id ? "filter-option active" : "filter-option"} onClick={() => update("category", item.id)}>{item.name}<span>{item.gameCount ?? 0}</span></button>)}
          <hr />
          <label className="toggle-filter"><input type="checkbox" checked={featured === "true"} onChange={(event) => update("featured", event.target.checked ? "true" : "")} /><span /> 只看编辑精选</label>
          {(query || category || featured) && <button className="clear-filters" onClick={clearFilters}><X size={15} /> 清除全部筛选</button>}
        </aside>
        <section className="catalog-results" aria-busy={games.loading}>
          {query && <p className="search-context">关于“<strong>{query}</strong>”的搜索结果</p>}
          {games.loading && <LoadingState label="正在翻阅目录" />}
          {games.error && <ErrorState message={games.error} retry={games.reload} />}
          {games.data && games.data.items.length === 0 && <div className="empty-state"><span>?</span><h2>没有找到同频的游戏</h2><p>换一个关键词，或者清除筛选再看看。</p><button className="button button--ghost" onClick={clearFilters}>回到完整目录</button></div>}
          {games.data && games.data.items.length > 0 && <div className={view === "list" ? "game-grid game-grid--list" : "game-grid"}>{games.data.items.map((game, index) => <GameCard game={game} index={index} variant={view} metric={sort === "likes" ? "likes" : sort === "plays" || sort === "recommended" ? "plays" : "favorites"} key={game.id} />)}</div>}
          {games.data && games.data.total > games.data.pageSize && <div className="pagination"><button disabled={page <= 1} onClick={() => update("page", String(page - 1))}>上一页</button><span>{page} / {Math.ceil(games.data.total / games.data.pageSize)}</span><button disabled={page >= Math.ceil(games.data.total / games.data.pageSize)} onClick={() => update("page", String(page + 1))}>下一页</button></div>}
        </section>
      </div>
    </div>
  );
}

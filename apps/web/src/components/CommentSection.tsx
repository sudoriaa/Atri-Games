import type { GameComment } from "@atri/shared";
import { Flag, Heart, MessageSquare, Reply, Send, Trash2 } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useAuth } from "../lib/auth";
import { UserAvatar } from "./UserAvatar";
import { ReportDialog } from "./ReportDialog";

const PAGE_SIZE = 20;
const MAX_BODY = 1000;

interface CommentSectionProps {
  slug: string;
  /** Lets the detail page keep its header counter in step with the thread. */
  onCountChange: (delta: number) => void;
}

function formatTime(value: string) {
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) return value;
  const elapsed = Date.now() - parsed.getTime();
  const minutes = Math.floor(elapsed / 60_000);
  if (minutes < 1) return "刚刚";
  if (minutes < 60) return `${minutes} 分钟前`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours} 小时前`;
  const days = Math.floor(hours / 24);
  if (days < 30) return `${days} 天前`;
  return parsed.toLocaleDateString("zh-CN");
}

/** Applies `update` to the matching comment in either nesting level. */
function patchComment(items: GameComment[], id: string, update: (item: GameComment) => GameComment) {
  return items.map((item) => {
    if (item.id === id) return update(item);
    if (item.replies?.length) {
      return { ...item, replies: item.replies.map((reply) => (reply.id === id ? update(reply) : reply)) };
    }
    return item;
  });
}

export function CommentSection({ slug, onCountChange }: CommentSectionProps) {
  const { api, user } = useAuth();
  const navigate = useNavigate();
  const [items, setItems] = useState<GameComment[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [body, setBody] = useState("");
  const [posting, setPosting] = useState(false);
  const [replyTo, setReplyTo] = useState<string | null>(null);
  const [replyBody, setReplyBody] = useState("");
  const [busyId, setBusyId] = useState("");
  const [reporting, setReporting] = useState<GameComment | null>(null);

  const load = useCallback(
    async (targetPage: number) => {
      setLoading(true);
      setError("");
      try {
        const response = await api.gameComments(slug, { page: targetPage, pageSize: PAGE_SIZE });
        setItems(response.items);
        setTotal(response.total);
        setPage(response.page);
      } catch (caught) {
        setError(caught instanceof Error ? caught.message : "留言加载失败");
      } finally {
        setLoading(false);
      }
    },
    [api, slug],
  );

  useEffect(() => {
    void load(1);
  }, [load]);

  const requireLogin = useCallback(() => {
    navigate(`/auth?next=${encodeURIComponent(`/games/${slug}`)}`);
  }, [navigate, slug]);

  const submit = useCallback(
    async (text: string, parentId?: string) => {
      if (!user) {
        requireLogin();
        return false;
      }
      const trimmed = text.trim();
      if (!trimmed) return false;
      setPosting(true);
      setNotice("");
      try {
        const created = await api.addGameComment(slug, trimmed, parentId);
        if (parentId) {
          setItems((current) =>
            patchComment(current, parentId, (item) => ({
              ...item,
              replyCount: item.replyCount + 1,
              replies: [...(item.replies ?? []), created],
            })),
          );
        } else {
          setItems((current) => [created, ...current]);
          setTotal((current) => current + 1);
        }
        onCountChange(1);
        return true;
      } catch (caught) {
        setNotice(caught instanceof Error ? caught.message : "留言发送失败");
        return false;
      } finally {
        setPosting(false);
      }
    },
    [api, onCountChange, requireLogin, slug, user],
  );

  const remove = useCallback(
    async (comment: GameComment) => {
      setBusyId(comment.id);
      setNotice("");
      try {
        await api.deleteGameComment(slug, comment.id);
        const removed = 1 + (comment.replies?.length ?? 0);
        if (comment.parentId) {
          setItems((current) =>
            patchComment(current, comment.parentId!, (item) => ({
              ...item,
              replyCount: Math.max(0, item.replyCount - 1),
              replies: (item.replies ?? []).filter((reply) => reply.id !== comment.id),
            })),
          );
        } else {
          setItems((current) => current.filter((item) => item.id !== comment.id));
          setTotal((current) => Math.max(0, current - 1));
        }
        onCountChange(-removed);
      } catch (caught) {
        setNotice(caught instanceof Error ? caught.message : "删除失败");
      } finally {
        setBusyId("");
      }
    },
    [api, onCountChange, slug],
  );

  const toggleLike = useCallback(
    async (comment: GameComment) => {
      if (!user) {
        requireLogin();
        return;
      }
      setBusyId(comment.id);
      try {
        const state = comment.isLiked
          ? await api.unlikeGameComment(slug, comment.id)
          : await api.likeGameComment(slug, comment.id);
        setItems((current) =>
          patchComment(current, comment.id, (item) => ({
            ...item,
            isLiked: state.isLiked,
            likeCount: state.likeCount,
          })),
        );
      } catch (caught) {
        setNotice(caught instanceof Error ? caught.message : "操作失败");
      } finally {
        setBusyId("");
      }
    },
    [api, requireLogin, slug, user],
  );

  const pages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  const renderComment = (comment: GameComment, isReply = false) => (
    <article key={comment.id} className={`comment${isReply ? " comment--reply" : ""}`}>
      <UserAvatar
        className="comment__avatar"
        name={comment.authorName}
        src={comment.authorAvatarUrl}
        decorative
      />
      <div className="comment__main">
        <div className="comment__meta">
          <Link to={`/creators/${comment.authorId}`}><strong>{comment.authorName}</strong></Link>
          {comment.authorUserNumber > 0 && <span className="comment__user-id">用户 ID: {comment.authorUserNumber}</span>}
          {comment.authorRole === "admin" && <span className="comment__badge">管理员</span>}
          <time dateTime={comment.createdAt}>{formatTime(comment.createdAt)}</time>
        </div>
        <p className="comment__body">{comment.body}</p>
        <div className="comment__actions">
          <button
            className={`comment__action${comment.isLiked ? " is-active" : ""}`}
            onClick={() => void toggleLike(comment)}
            disabled={busyId === comment.id}
            aria-label={comment.isLiked ? "取消点赞该留言" : "点赞该留言"}
          >
            <Heart size={13} fill={comment.isLiked ? "currentColor" : "none"} />
            {comment.likeCount > 0 && comment.likeCount}
          </button>
          {!isReply && (
            <button
              className="comment__action"
              onClick={() => {
                if (!user) {
                  requireLogin();
                  return;
                }
                setReplyTo(replyTo === comment.id ? null : comment.id);
                setReplyBody("");
              }}
            >
              <Reply size={13} /> 回复{comment.replyCount > 0 ? ` ${comment.replyCount}` : ""}
            </button>
          )}
          {comment.canDelete && (
            <button
              className="comment__action"
              onClick={() => void remove(comment)}
              disabled={busyId === comment.id}
            >
              <Trash2 size={13} /> 删除
            </button>
          )}
          {!comment.canDelete && <button className="comment__action" onClick={() => user ? setReporting(comment) : requireLogin()}><Flag size={13} /> 举报</button>}
        </div>

        {replyTo === comment.id && (
          <form
            className="comment-form comment-form--reply"
            onSubmit={async (event) => {
              event.preventDefault();
              if (await submit(replyBody, comment.id)) {
                setReplyBody("");
                setReplyTo(null);
              }
            }}
          >
            <textarea
              value={replyBody}
              onChange={(event) => setReplyBody(event.target.value)}
              placeholder={`回复 ${comment.authorName}…`}
              maxLength={MAX_BODY}
              rows={2}
              aria-label={`回复 ${comment.authorName}`}
            />
            <div className="comment-form__foot">
              <button type="button" className="button button--small button--ghost" onClick={() => setReplyTo(null)}>
                取消
              </button>
              <button className="button button--small" disabled={posting || !replyBody.trim()}>
                <Send size={14} /> 回复
              </button>
            </div>
          </form>
        )}

        {comment.replies?.map((reply) => renderComment(reply, true))}
      </div>
    </article>
  );

  return (
    <section className="comment-section" aria-labelledby="comment-heading">
      <div className="comment-section__head">
        <p className="kicker"><MessageSquare size={13} /> DISCUSSION</p>
        <h2 id="comment-heading">玩家留言 {total > 0 && <small>{total}</small>}</h2>
      </div>

      {user ? (
        <form
          className="comment-form"
          onSubmit={async (event) => {
            event.preventDefault();
            if (await submit(body)) setBody("");
          }}
        >
          <textarea
            value={body}
            onChange={(event) => setBody(event.target.value)}
            placeholder="说说你的体验，或给创作者一点反馈…"
            maxLength={MAX_BODY}
            rows={3}
            aria-label="发表留言"
          />
          <div className="comment-form__foot">
            <small>{body.length} / {MAX_BODY}</small>
            <button className="button button--small" disabled={posting || !body.trim()}>
              <Send size={14} /> {posting ? "发送中…" : "发表留言"}
            </button>
          </div>
        </form>
      ) : (
        <p className="comment-section__guest">
          <button className="text-link" onClick={requireLogin}>登录后即可留言</button>
        </p>
      )}

      {notice && <p className="inline-notice">{notice}</p>}

      {loading ? (
        <p className="comment-section__empty">正在加载留言…</p>
      ) : error ? (
        <p className="comment-section__empty">
          {error} <button className="text-link" onClick={() => void load(page)}>重试</button>
        </p>
      ) : items.length === 0 ? (
        <p className="comment-section__empty">还没有留言，来做第一个留言的玩家。</p>
      ) : (
        <div className="comment-list">{items.map((item) => renderComment(item))}</div>
      )}

      {pages > 1 && (
        <div className="pagination">
          <button disabled={page <= 1 || loading} onClick={() => void load(page - 1)}>上一页</button>
          <span>{page} / {pages}</span>
          <button disabled={page >= pages || loading} onClick={() => void load(page + 1)}>下一页</button>
        </div>
      )}
      {reporting && <ReportDialog targetType="comment" targetId={reporting.id} targetLabel={`${reporting.authorName} 的留言`} onClose={() => setReporting(null)} />}
    </section>
  );
}

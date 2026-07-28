import type { Game, GameShareChannel } from "@atri/shared";
import { Check, Copy, Download, Link2, QrCode, Share2, X } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { encodeQR, qrToSvgPath } from "../lib/qrcode";

const CARD_WIDTH = 720;
const CARD_HEIGHT = 1020;

interface ShareDialogProps {
  game: Game;
  shareUrl: string;
  onClose: () => void;
  /** Reports a completed share so the page can settle its counter. */
  onShared: (channel: GameShareChannel) => void;
}

/** Wraps text to the given pixel width and returns at most `maxLines` lines. */
function wrapText(ctx: CanvasRenderingContext2D, text: string, maxWidth: number, maxLines: number) {
  const lines: string[] = [];
  let current = "";
  // CJK text has no spaces to break on, so wrap per character. Latin words stay
  // intact because a space always closes the current line first.
  for (const char of text) {
    const candidate = current + char;
    if (ctx.measureText(candidate).width > maxWidth && current) {
      lines.push(current);
      current = char === " " ? "" : char;
      if (lines.length === maxLines) return lines;
    } else {
      current = candidate;
    }
  }
  if (current && lines.length < maxLines) lines.push(current);
  if (lines.length === maxLines) {
    const last = lines[maxLines - 1];
    if (ctx.measureText(last).width > maxWidth - 24) {
      lines[maxLines - 1] = `${last.slice(0, -1)}…`;
    }
  }
  return lines;
}

function loadCover(url: string): Promise<HTMLImageElement | null> {
  return new Promise((resolve) => {
    const image = new Image();
    // Managed covers are same-origin; the request stays anonymous so an
    // external cover can still paint without tainting the canvas when its host
    // sends permissive CORS headers.
    image.crossOrigin = "anonymous";
    image.onload = () => resolve(image);
    image.onerror = () => resolve(null);
    image.src = url;
  });
}

export function ShareDialog({ game, shareUrl, onClose, onShared }: ShareDialogProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const closeRef = useRef<HTMLButtonElement>(null);
  const [copied, setCopied] = useState(false);
  const [cardReady, setCardReady] = useState(false);
  const [cardError, setCardError] = useState("");
  const [notice, setNotice] = useState("");

  const qr = useMemo(() => {
    try {
      return encodeQR(shareUrl);
    } catch {
      return null;
    }
  }, [shareUrl]);

  const qrPath = useMemo(() => (qr ? qrToSvgPath(qr) : ""), [qr]);

  useEffect(() => {
    closeRef.current?.focus();
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [onClose]);

  // Paint the share card once the dialog opens.
  useEffect(() => {
    let cancelled = false;
    const paint = async () => {
      const canvas = canvasRef.current;
      if (!canvas || !qr) return;
      const ctx = canvas.getContext("2d");
      if (!ctx) return;

      canvas.width = CARD_WIDTH;
      canvas.height = CARD_HEIGHT;

      ctx.fillStyle = "#f2efe7";
      ctx.fillRect(0, 0, CARD_WIDTH, CARD_HEIGHT);

      const cover = await loadCover(game.coverUrl);
      if (cancelled) return;

      const artHeight = 470;
      if (cover) {
        // Cover-fit the artwork into the top panel.
        const scale = Math.max(CARD_WIDTH / cover.width, artHeight / cover.height);
        const width = cover.width * scale;
        const height = cover.height * scale;
        ctx.save();
        ctx.beginPath();
        ctx.rect(0, 0, CARD_WIDTH, artHeight);
        ctx.clip();
        ctx.drawImage(cover, (CARD_WIDTH - width) / 2, (artHeight - height) / 2, width, height);
        ctx.restore();
      } else {
        ctx.fillStyle = "#7d66ff";
        ctx.fillRect(0, 0, CARD_WIDTH, artHeight);
      }

      ctx.fillStyle = "#171614";
      ctx.fillRect(0, artHeight, CARD_WIDTH, 3);

      ctx.fillStyle = "#ccff33";
      ctx.fillRect(44, artHeight - 52, 190, 34);
      ctx.fillStyle = "#171614";
      ctx.font = "700 15px monospace";
      ctx.fillText("ATRI GAMES", 58, artHeight - 29);

      let cursor = artHeight + 74;
      ctx.fillStyle = "#716e67";
      ctx.font = "700 17px monospace";
      ctx.fillText(game.categoryName.toUpperCase(), 44, cursor);

      cursor += 58;
      ctx.fillStyle = "#171614";
      ctx.font = "500 54px Georgia, serif";
      for (const line of wrapText(ctx, game.title, CARD_WIDTH - 88, 2)) {
        ctx.fillText(line, 44, cursor);
        cursor += 62;
      }

      cursor += 6;
      ctx.fillStyle = "#48453f";
      ctx.font = "400 22px Inter, system-ui, sans-serif";
      for (const line of wrapText(ctx, game.summary, CARD_WIDTH - 300, 3)) {
        ctx.fillText(line, 44, cursor);
        cursor += 33;
      }

      ctx.fillStyle = "#716e67";
      ctx.font = "400 19px Inter, system-ui, sans-serif";
      ctx.fillText(`by ${game.authorName}`, 44, CARD_HEIGHT - 62);

      // Quiet zone is already part of the matrix, so the code can sit flush.
      const qrSize = 190;
      const module = qrSize / qr.length;
      const qrX = CARD_WIDTH - qrSize - 44;
      const qrY = CARD_HEIGHT - qrSize - 44;
      ctx.fillStyle = "#ffffff";
      ctx.fillRect(qrX, qrY, qrSize, qrSize);
      ctx.fillStyle = "#171614";
      for (let row = 0; row < qr.length; row += 1) {
        for (let col = 0; col < qr[row].length; col += 1) {
          if (qr[row][col]) {
            ctx.fillRect(qrX + col * module, qrY + row * module, module + 0.5, module + 0.5);
          }
        }
      }

      ctx.strokeStyle = "#171614";
      ctx.lineWidth = 6;
      ctx.strokeRect(3, 3, CARD_WIDTH - 6, CARD_HEIGHT - 6);

      if (!cancelled) setCardReady(true);
    };

    paint().catch(() => {
      if (!cancelled) setCardError("分享图生成失败，可改用链接或二维码");
    });
    return () => {
      cancelled = true;
    };
  }, [game.authorName, game.categoryName, game.coverUrl, game.summary, game.title, qr]);

  const copyLink = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(shareUrl);
      setCopied(true);
      setNotice("");
      onShared("link");
      window.setTimeout(() => setCopied(false), 2200);
    } catch {
      setNotice("浏览器阻止了剪贴板访问，请手动复制上方链接");
    }
  }, [onShared, shareUrl]);

  const nativeShare = useCallback(async () => {
    if (!navigator.share) return;
    try {
      await navigator.share({ title: game.title, text: game.summary, url: shareUrl });
      onShared("native");
    } catch {
      // The player dismissed the sheet; nothing to report.
    }
  }, [game.summary, game.title, onShared, shareUrl]);

  const downloadCard = useCallback(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    try {
      const url = canvas.toDataURL("image/png");
      const anchor = document.createElement("a");
      anchor.href = url;
      anchor.download = `atri-${game.slug}.png`;
      anchor.click();
      onShared("card");
    } catch {
      // A cross-origin cover without CORS headers taints the canvas.
      setCardError("该游戏封面来自外部站点，无法导出分享图；请改用链接或二维码");
    }
  }, [game.slug, onShared]);

  return (
    <div className="share-overlay" role="presentation" onClick={onClose}>
      <div
        className="share-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="share-dialog-title"
        onClick={(event) => event.stopPropagation()}
      >
        <header className="share-dialog__head">
          <div>
            <p className="kicker"><Share2 size={13} /> SHARE</p>
            <h2 id="share-dialog-title">分享《{game.title}》</h2>
          </div>
          <button ref={closeRef} className="icon-button" onClick={onClose} aria-label="关闭分享面板">
            <X />
          </button>
        </header>

        <div className="share-dialog__body">
          <div className="share-dialog__preview">
            {/* The canvas is the downloadable artefact and the visual preview. */}
            <canvas ref={canvasRef} aria-label={`${game.title} 分享卡片预览`} />
            {!cardReady && !cardError && <p className="share-dialog__pending">正在生成分享图…</p>}
          </div>

          <div className="share-dialog__controls">
            <section>
              <h3><Link2 size={15} /> 游戏链接</h3>
              <div className="share-dialog__link">
                <input value={shareUrl} readOnly aria-label="游戏分享链接" onFocus={(e) => e.currentTarget.select()} />
                <button className="button button--small" onClick={copyLink}>
                  {copied ? <><Check size={15} /> 已复制</> : <><Copy size={15} /> 复制</>}
                </button>
              </div>
            </section>

            <section>
              <h3><QrCode size={15} /> 扫码即玩</h3>
              {qr ? (
                <svg
                  className="share-dialog__qr"
                  viewBox={`0 0 ${qr.length} ${qr.length}`}
                  role="img"
                  aria-label={`${game.title} 的游戏地址二维码`}
                  shapeRendering="crispEdges"
                >
                  <rect width={qr.length} height={qr.length} fill="#ffffff" />
                  <path d={qrPath} fill="#171614" />
                </svg>
              ) : (
                <p className="share-dialog__pending">链接过长，无法生成二维码</p>
              )}
            </section>

            <div className="share-dialog__actions">
              <button className="button button--wide" onClick={downloadCard} disabled={!cardReady}>
                <Download size={16} /> 下载分享图
              </button>
              {typeof navigator !== "undefined" && "share" in navigator && (
                <button className="button button--wide button--ghost" onClick={nativeShare}>
                  <Share2 size={16} /> 系统分享
                </button>
              )}
            </div>

            {cardError && <p className="inline-notice">{cardError}</p>}
            {notice && <p className="inline-notice">{notice}</p>}
          </div>
        </div>
      </div>
    </div>
  );
}

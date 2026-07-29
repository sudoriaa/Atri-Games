import { useEffect, useState } from "react";

interface UserAvatarProps {
  name: string;
  src?: string | null;
  className: string;
  decorative?: boolean;
}

export function userInitials(name: string) {
  return name.trim().slice(0, 1).toUpperCase() || "?";
}

/** Renders an uploaded avatar while preserving a deterministic initials fallback. */
export function UserAvatar({ name, src, className, decorative = false }: UserAvatarProps) {
  const normalizedSrc = src?.trim() ?? "";
  const [failedSrc, setFailedSrc] = useState("");
  const imageSrc = normalizedSrc && normalizedSrc !== failedSrc ? normalizedSrc : "";
  const label = `${name || "用户"}的头像`;

  useEffect(() => {
    setFailedSrc("");
  }, [normalizedSrc]);

  return (
    <span className={className}>
      {imageSrc ? (
        <img src={imageSrc} alt={decorative ? "" : label} onError={() => setFailedSrc(normalizedSrc)} />
      ) : (
        <span {...(decorative ? { "aria-hidden": true } : { role: "img", "aria-label": label })}>
          {userInitials(name)}
        </span>
      )}
    </span>
  );
}

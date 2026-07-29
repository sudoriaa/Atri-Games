import { useEffect, useState } from "react";

interface UserAvatarProps {
  name: string;
  src?: string | null;
}

function initials(name: string) {
  return name.trim().slice(0, 1).toUpperCase() || "?";
}

export function UserAvatar({ name, src }: UserAvatarProps) {
  const normalizedSrc = src?.trim() ?? "";
  const [failedSrc, setFailedSrc] = useState("");
  const imageSrc = normalizedSrc && normalizedSrc !== failedSrc ? normalizedSrc : "";

  useEffect(() => {
    setFailedSrc("");
  }, [normalizedSrc]);

  return (
    <span className="user-avatar" aria-hidden="true">
      {imageSrc ? <img src={imageSrc} alt="" onError={() => setFailedSrc(normalizedSrc)} /> : <span>{initials(name)}</span>}
    </span>
  );
}

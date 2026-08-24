import { useEffect, useState } from "react";

import { branding } from "@/config/branding";

export function HeroMedia() {
  const source = branding.heroMedia;
  const isVideo = branding.heroIsVideo;
  const [videoSrc, setVideoSrc] = useState<string | null>(null);

  useEffect(() => {
    if (!isVideo) {
      setVideoSrc(null);
      return;
    }
    if (source.startsWith("data:") || source.startsWith("blob:")) {
      setVideoSrc(source);
      return;
    }
    let objectUrl: string | null = null;
    let cancelled = false;
    fetch(source)
      .then((response) => response.blob())
      .then((blob) => {
        if (cancelled) return;
        objectUrl = URL.createObjectURL(blob);
        setVideoSrc(objectUrl);
      })
      .catch(() => {
        if (!cancelled) setVideoSrc(source);
      });
    return () => {
      cancelled = true;
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [source, isVideo]);

  return (
    <div className="pointer-events-none absolute inset-0 overflow-hidden">
      {isVideo ? (
        videoSrc && (
          <video
            className="absolute inset-0 h-full w-full object-cover"
            src={videoSrc}
            autoPlay
            loop
            muted
            playsInline
          />
        )
      ) : (
        <img className="absolute inset-0 h-full w-full object-cover" src={source} alt="" />
      )}
      <div className="absolute inset-0" style={{ background: "var(--lm-scrim-x)" }} />
      <div className="absolute inset-0" style={{ background: "var(--lm-scrim-y)" }} />
      <div className="absolute inset-0" style={{ background: "var(--lm-scrim-top)" }} />
    </div>
  );
}

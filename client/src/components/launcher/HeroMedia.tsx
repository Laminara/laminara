import { branding } from "@/config/branding";

export function HeroMedia() {
  return (
    <div className="pointer-events-none absolute inset-0 overflow-hidden">
      <video
        className="absolute inset-0 h-full w-full object-cover"
        src={branding.heroMedia}
        autoPlay
        loop
        muted
        playsInline
      />
      <div className="absolute inset-0" style={{ background: "var(--lm-scrim-x)" }} />
      <div className="absolute inset-0" style={{ background: "var(--lm-scrim-y)" }} />
      <div className="absolute inset-0" style={{ background: "var(--lm-scrim-top)" }} />
    </div>
  );
}

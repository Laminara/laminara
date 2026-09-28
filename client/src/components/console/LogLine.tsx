import type { GameLevel, GameLine } from "@/lib/types";
import { highlightParts } from "@/lib/gameLog";
import { cn } from "@/lib/format";

export const logTextClass = "font-mono text-[12px] leading-(--lm-log-line) [tab-size:4]";

const levelTone: Record<GameLevel, string> = {
  debug: "text-dim",
  info: "text-text/85",
  warn: "text-warn",
  error: "bg-danger/[0.07] text-danger shadow-[inset_2px_0_0_var(--lm-danger)]",
};

export function LogLine({ line, needle = "" }: { line: GameLine; needle?: string }) {
  return (
    <div className={cn("min-h-(--lm-log-line) whitespace-pre-wrap px-4 [overflow-wrap:anywhere]", levelTone[line.level])}>
      {needle
        ? highlightParts(line.text, needle).map((part, index) =>
            part.hit ? (
              <mark key={index} className="bg-primary/30 text-text">
                {part.text}
              </mark>
            ) : (
              part.text
            ),
          )
        : line.text}
    </div>
  );
}

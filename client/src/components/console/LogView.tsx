import { createContext, memo, useCallback, useContext, useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { ArrowDown, MagnifyingGlass, TerminalWindow } from "@phosphor-icons/react";
import type { GameLine } from "@/lib/types";
import { useConsole } from "@/consoleStore";
import { chunkKey } from "@/lib/gameLog";
import { cn, formatCount, plural } from "@/lib/format";
import { Button } from "@/components/ui/Button";
import { LogLine, logTextClass } from "./LogLine";

const FOLLOW_SLACK_PX = 24;
const EAGER_CHUNKS = 3;
const MOUNT_MARGIN = "1500px 0px";
const INTENT_WINDOW_MS = 700;
const SCROLL_BACK_KEYS = new Set(["ArrowUp", "PageUp", "Home"]);

const ScrollRoot = createContext<HTMLElement | null>(null);

const Chunk = memo(function Chunk({ lines, needle, eager }: { lines: GameLine[]; needle: string; eager: boolean }) {
  const root = useContext(ScrollRoot);
  const holder = useRef<HTMLDivElement>(null);
  const [reached, setReached] = useState(eager);
  if (eager && !reached) setReached(true);
  const mounted = reached || eager;
  const height = `calc(${lines.length} * var(--lm-log-line))`;

  useEffect(() => {
    const element = holder.current;
    if (mounted || !root || !element) return;
    const observer = new IntersectionObserver((entries) => entries.some((entry) => entry.isIntersecting) && setReached(true), {
      root,
      rootMargin: MOUNT_MARGIN,
    });
    observer.observe(element);
    return () => observer.disconnect();
  }, [mounted, root]);

  if (!mounted) return <div ref={holder} style={{ height }} />;
  return (
    <div style={{ contentVisibility: "auto", containIntrinsicSize: `auto ${height}` }}>
      {lines.map((line) => (
        <LogLine key={line.seq} line={line} needle={needle} />
      ))}
    </div>
  );
});

function Placeholder({ icon, title, text, action }: { icon: ReactNode; title: string; text: string; action?: ReactNode }) {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 px-8 text-center">
      <span className="flex h-14 w-14 items-center justify-center rounded-full border border-border bg-surface text-dim">{icon}</span>
      <div>
        <p className="text-[15px] font-semibold">{title}</p>
        <p className="mt-1 max-w-sm text-sm text-dim">{text}</p>
      </div>
      {action}
    </div>
  );
}

interface LogViewProps {
  visible: GameLine[][];
  shown: number;
  needle: string;
}

export function LogView({ visible, shown, needle }: LogViewProps) {
  const ready = useConsole((state) => state.ready);
  const session = useConsole((state) => state.session);
  const total = useConsole((state) => state.total);
  const filter = useConsole((state) => state.filter);
  const setFilter = useConsole((state) => state.setFilter);
  const setQuery = useConsole((state) => state.setQuery);
  const scroller = useRef<HTMLDivElement | null>(null);
  const content = useRef<HTMLDivElement>(null);
  const [root, setRoot] = useState<HTMLDivElement | null>(null);
  const [follow, setFollow] = useState(true);
  const following = useRef(true);
  const [unseen, setUnseen] = useState(0);
  const seen = useRef({ shown, session, view: "" });
  const intent = useRef(0);
  const holding = useRef(false);

  const view = `${filter}\u0000${needle}`;

  const attachScroller = useCallback((node: HTMLDivElement | null) => {
    scroller.current = node;
    setRoot(node);
  }, []);

  const setFollowing = useCallback((value: boolean) => {
    following.current = value;
    setFollow(value);
  }, []);

  const pinIfFollowing = useCallback(() => {
    const element = scroller.current;
    if (element && following.current && !holding.current) element.scrollTop = element.scrollHeight;
  }, []);

  useLayoutEffect(() => {
    const previous = seen.current;
    seen.current = { shown, session, view };
    const restarted = previous.session !== session;
    if (restarted || previous.view !== view) setUnseen(0);
    if (restarted) setFollowing(true);
    if (following.current) pinIfFollowing();
    else if (previous.view === view && shown > previous.shown) setUnseen((count) => count + shown - previous.shown);
  }, [visible, shown, session, view, root, setFollowing, pinIfFollowing]);

  useEffect(() => {
    if (!root || !content.current) return;
    const observer = new ResizeObserver(pinIfFollowing);
    observer.observe(content.current);
    observer.observe(root);
    return () => observer.disconnect();
  }, [root, pinIfFollowing]);

  useEffect(() => {
    const release = () => {
      holding.current = false;
      pinIfFollowing();
    };
    window.addEventListener("pointerup", release);
    window.addEventListener("pointercancel", release);
    return () => {
      window.removeEventListener("pointerup", release);
      window.removeEventListener("pointercancel", release);
    };
  }, [pinIfFollowing]);

  const markIntent = () => {
    intent.current = performance.now();
  };

  const onScroll = () => {
    const element = scroller.current;
    if (!element) return;
    if (element.scrollHeight - element.scrollTop - element.clientHeight <= FOLLOW_SLACK_PX) {
      setFollowing(true);
      setUnseen(0);
      return;
    }
    if (holding.current || performance.now() - intent.current < INTENT_WINDOW_MS) setFollowing(false);
  };

  const jumpToLatest = () => {
    setFollowing(true);
    setUnseen(0);
    pinIfFollowing();
  };

  if (!ready) return <div className="flex-1" />;

  let placeholder: ReactNode = null;
  if (session === 0) {
    placeholder = (
      <Placeholder
        icon={<TerminalWindow size={26} weight="duotone" />}
        title="Игра ещё не запускалась"
        text="Нажмите «Играть» в лаунчере — журнал игры появится здесь и будет обновляться сам."
      />
    );
  } else if (total === 0) {
    placeholder = (
      <Placeholder icon={<TerminalWindow size={26} weight="duotone" />} title="Ждём первых строк" text="Игра запускается, её журнал появится через пару секунд." />
    );
  } else if (shown === 0) {
    placeholder = (
      <Placeholder
        icon={<MagnifyingGlass size={24} />}
        title="Ничего не нашлось"
        text="Под этот поиск и фильтр не подходит ни одна строка."
        action={
          <Button
            variant="secondary"
            size="sm"
            onClick={() => {
              setQuery("");
              setFilter("all");
            }}
          >
            Показать весь журнал
          </Button>
        }
      />
    );
  }

  const pillHidden = follow || placeholder !== null;

  return (
    <div className="relative min-h-0 flex-1">
      {placeholder ?? (
        <div
          ref={attachScroller}
          tabIndex={0}
          aria-label="Журнал игры"
          onScroll={onScroll}
          onWheel={(event) => event.deltaY < 0 && markIntent()}
          onTouchMove={markIntent}
          onKeyDown={(event) => SCROLL_BACK_KEYS.has(event.key) && markIntent()}
          onPointerDown={() => {
            holding.current = true;
            markIntent();
          }}
          className={cn(
            "selectable h-full overflow-auto py-2 outline-none focus-visible:shadow-[inset_0_0_0_1px_var(--lm-border-strong)]",
            logTextClass,
          )}
        >
          <ScrollRoot.Provider value={root}>
            <div ref={content}>
              {visible.map((lines, index) =>
                lines.length > 0 ? (
                  <Chunk key={chunkKey(lines)} lines={lines} needle={needle} eager={index >= visible.length - EAGER_CHUNKS} />
                ) : null,
              )}
            </div>
          </ScrollRoot.Provider>
        </div>
      )}
      <button
        onClick={jumpToLatest}
        tabIndex={pillHidden ? -1 : 0}
        aria-hidden={pillHidden}
        className={cn(
          "absolute bottom-4 left-1/2 flex h-9 -translate-x-1/2 items-center gap-2 rounded-full border border-border-strong bg-panel-2 px-4 text-sm font-medium text-text shadow-panel backdrop-blur-xl transition-[opacity,transform,background-color] duration-200 ease-out hover:bg-surface-3 motion-reduce:transition-none",
          pillHidden ? "pointer-events-none translate-y-2 opacity-0" : "translate-y-0 opacity-100",
        )}
      >
        <ArrowDown size={14} weight="bold" />
        {unseen > 0 ? `${formatCount(unseen)} ${plural(unseen, "новая строка", "новые строки", "новых строк")}` : "К последним строкам"}
      </button>
    </div>
  );
}

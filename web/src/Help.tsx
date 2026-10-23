import { useEffect, useId, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { glossaryText } from "./glossary";

const useBrowserLayoutEffect = typeof document === "undefined" ? useEffect : useLayoutEffect;

export function Help({ term }: { term: string }) {
  const text = glossaryText(term);
  const id = useId();
  const button = useRef<HTMLButtonElement>(null);
  const bubble = useRef<HTMLDivElement>(null);
  const timer = useRef<ReturnType<typeof setTimeout>>();
  const [open, setOpen] = useState(false);
  const [position, setPosition] = useState({ left: 12, top: 12 });
  function show() {
    clearTimeout(timer.current);
    setOpen(true);
  }
  function hide() {
    clearTimeout(timer.current);
    setOpen(false);
  }
  function leave() {
    timer.current = setTimeout(() => setOpen(false), 120);
  }
  useEffect(() => () => clearTimeout(timer.current), []);
  useBrowserLayoutEffect(() => {
    if (!open || !button.current || !bubble.current) return;
    const anchor = button.current.getBoundingClientRect();
    const box = bubble.current.getBoundingClientRect();
    const left = Math.max(12, Math.min(anchor.left, window.innerWidth - box.width - 12));
    const top =
      anchor.bottom + box.height + 10 < window.innerHeight
        ? anchor.bottom + 6
        : Math.max(12, anchor.top - box.height - 6);
    setPosition({ left, top });
  }, [open]);
  useEffect(() => {
    if (!open) return;
    const escape = (event: KeyboardEvent) => {
      if (event.key === "Escape") hide();
    };
    const outside = (event: PointerEvent) => {
      if (
        !button.current?.contains(event.target as Node) &&
        !bubble.current?.contains(event.target as Node)
      )
        hide();
    };
    document.addEventListener("keydown", escape);
    document.addEventListener("pointerdown", outside);
    window.addEventListener("resize", hide);
    window.addEventListener("scroll", hide, true);
    return () => {
      document.removeEventListener("keydown", escape);
      document.removeEventListener("pointerdown", outside);
      window.removeEventListener("resize", hide);
      window.removeEventListener("scroll", hide, true);
    };
  }, [open]);
  if (!text) return null;
  return (
    <>
      <button
        ref={button}
        type="button"
        className="help-button"
        aria-label={`About ${term}`}
        aria-describedby={id}
        aria-expanded={open}
        onPointerEnter={(event) => {
          if (event.pointerType === "mouse") show();
        }}
        onPointerLeave={(event) => {
          if (event.pointerType === "mouse") leave();
        }}
        onFocus={(event) => {
          if (event.currentTarget.matches(":focus-visible")) show();
        }}
        onBlur={hide}
        onClick={() => {
          clearTimeout(timer.current);
          setOpen((value) => !value);
        }}
      >
        <span aria-hidden="true">?</span>
      </button>
      {typeof document !== "undefined" &&
        createPortal(
          <div
            ref={bubble}
            id={id}
            role="tooltip"
            className="tooltip"
            hidden={!open}
            style={position}
            onMouseEnter={() => clearTimeout(timer.current)}
            onMouseLeave={leave}
          >
            {text}
          </div>,
          document.body,
        )}
    </>
  );
}

export function Label({ children, term }: { children: ReactNode; term?: string }) {
  const key = term ?? (typeof children === "string" ? children : "");
  return (
    <span className="help-label">
      {children}
      <Help term={key} />
    </span>
  );
}

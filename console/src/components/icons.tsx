// Inline SVG icons. Decorative only (aria-hidden): every icon sits beside
// text that says the same thing, because colour and shape never carry
// meaning alone (WCAG 1.4.1; CONSOLE-DESIGN "Interaction baseline").

export type Tone = "success" | "warning" | "danger" | "info" | "neutral";

const common = {
  width: 16,
  height: 16,
  viewBox: "0 0 16 16",
  fill: "none",
  stroke: "currentColor",
  strokeWidth: 1.75,
  strokeLinecap: "round" as const,
  strokeLinejoin: "round" as const,
  "aria-hidden": true,
  focusable: false,
};

export function ToneIcon({ tone }: { tone: Tone }) {
  switch (tone) {
    case "success":
      return (
        <svg {...common} className="icon">
          <circle cx="8" cy="8" r="6.5" />
          <path d="M5 8.2l2 2 4-4.4" />
        </svg>
      );
    case "warning":
      return (
        <svg {...common} className="icon">
          <path d="M8 1.8l6.6 11.6H1.4z" />
          <path d="M8 6.2v3.2M8 11.6v.1" />
        </svg>
      );
    case "danger":
      return (
        <svg {...common} className="icon">
          <circle cx="8" cy="8" r="6.5" />
          <path d="M5.6 5.6l4.8 4.8M10.4 5.6l-4.8 4.8" />
        </svg>
      );
    case "info":
      return (
        <svg {...common} className="icon">
          <circle cx="8" cy="8" r="6.5" />
          <path d="M8 7.2v4M8 4.8v.1" />
        </svg>
      );
    case "neutral":
    default:
      return (
        <svg {...common} className="icon">
          <circle cx="8" cy="8" r="6.5" strokeDasharray="2.5 2" />
        </svg>
      );
  }
}

export function MenuIcon() {
  return (
    <svg {...common} width={20} height={20} viewBox="0 0 20 20" className="icon">
      <path d="M3 5.5h14M3 10h14M3 14.5h14" />
    </svg>
  );
}

export function CloseIcon() {
  return (
    <svg {...common} className="icon">
      <path d="M4 4l8 8M12 4l-8 8" />
    </svg>
  );
}

import { useId, type TextareaHTMLAttributes } from "react";

interface TextAreaFieldProps extends Omit<TextareaHTMLAttributes<HTMLTextAreaElement>, "id" | "className"> {
  label: string;
  hint?: string;
  // error is console copy shown under the field and announced with it.
  error?: string | null;
}

// TextAreaField is TextField for several lines: the same label, hint and
// error wiring (HLD-19 §7), for lists entered one item per line.
export function TextAreaField({ label, hint, error, ...area }: TextAreaFieldProps) {
  const id = useId();
  const hintId = hint ? `${id}-hint` : undefined;
  const errorId = error ? `${id}-error` : undefined;
  const describedBy = [hintId, errorId].filter(Boolean).join(" ") || undefined;
  return (
    <div className="field">
      <label className="field-label" htmlFor={id}>
        {label}
      </label>
      {hint ? (
        <p className="field-hint" id={hintId}>
          {hint}
        </p>
      ) : null}
      <textarea id={id} className="input input-multiline" aria-describedby={describedBy} aria-invalid={error ? true : undefined} {...area} />
      {error ? (
        <p className="field-error" id={errorId}>
          {error}
        </p>
      ) : null}
    </div>
  );
}

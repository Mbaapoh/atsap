import { useId, type InputHTMLAttributes, type Ref } from "react";

interface TextFieldProps extends Omit<InputHTMLAttributes<HTMLInputElement>, "id" | "className"> {
  label: string;
  hint?: string;
  // error is console copy shown under the field and announced with it.
  error?: string | null;
  inputRef?: Ref<HTMLInputElement>;
}

// TextField is a labelled input with its hint and error tied to it, so a
// screen reader reads them with the field and an invalid field says so
// (HLD-19 §7: validation errors stay near their fields).
export function TextField({ label, hint, error, inputRef, ...input }: TextFieldProps) {
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
      <input ref={inputRef} id={id} className="input" aria-describedby={describedBy} aria-invalid={error ? true : undefined} {...input} />
      {error ? (
        <p className="field-error" id={errorId}>
          {error}
        </p>
      ) : null}
    </div>
  );
}

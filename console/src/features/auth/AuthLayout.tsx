import { useEffect, useRef, type ReactNode } from "react";

// AuthLayout is the frame of every screen outside the shell: sign-in,
// recovery and Tenant choice. Its heading takes focus when the step
// changes, so keyboard and screen-reader users land on the new step.
export function AuthLayout({ title, children, stepKey }: { title: string; children: ReactNode; stepKey?: string }) {
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    document.title = `${title} · AtsaPBX`;
    heading.current?.focus();
    // stepKey is not read here on purpose: a new step under the same title
    // must still move focus to the heading, which is what listing it does.
    // oxlint-disable-next-line react/exhaustive-effect-dependencies
  }, [title, stepKey]);
  return (
    <main className="auth-layout">
      <section className="auth-card" aria-labelledby="page-title">
        <span className="product">AtsaPBX</span>
        <h1 id="page-title" ref={heading} tabIndex={-1}>
          {title}
        </h1>
        {children}
      </section>
    </main>
  );
}

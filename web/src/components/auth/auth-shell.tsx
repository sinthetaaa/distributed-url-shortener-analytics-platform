import Link from "next/link";

import styles from "./auth.module.css";

type AuthShellProps = {
  title: string;
  description: string;
  children: React.ReactNode;
  footerPrompt: string;
  footerLabel: string;
  footerHref: string;
};

export function AuthShell({
  title,
  description,
  children,
  footerPrompt,
  footerLabel,
  footerHref,
}: AuthShellProps) {
  return (
    <main className={styles.page}>
      <section className={styles.shell} aria-labelledby="auth-title">
        <div className={styles.brand}>ShortScale</div>

        <header className={styles.header}>
          <h1 id="auth-title" className={styles.title}>
            {title}
          </h1>
          <p className={styles.description}>{description}</p>
        </header>

        {children}

        <p className={styles.footer}>
          {footerPrompt}{" "}
          <Link className={styles.footerLink} href={footerHref}>
            {footerLabel}
          </Link>
        </p>
      </section>
    </main>
  );
}

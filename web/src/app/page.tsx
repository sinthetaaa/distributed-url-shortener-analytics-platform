import { LogoutButton } from "@/components/auth/logout-button";
import { ProtectedRoute } from "@/components/auth/protected-route";
import { ShortenForm } from "@/components/links/shorten-form";

import styles from "./page.module.css";

export default function Home() {
  return (
    <ProtectedRoute>
      <div className={styles.page}>
        <header className={styles.header}>
          <p className={styles.brand}>
            ShortScale
          </p>

          <LogoutButton />
        </header>

        <main className={styles.content}>
          <ShortenForm />
        </main>
      </div>
    </ProtectedRoute>
  );
}

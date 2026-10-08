import Link from "next/link";

import { LogoutButton } from "@/components/auth/logout-button";
import { ProtectedRoute } from "@/components/auth/protected-route";
import { ProductHome } from "@/components/links/product-home";

import styles from "./page.module.css";

export default function Home() {
  return (
    <ProtectedRoute>
      <div className={styles.page}>
        <header className={styles.header}>
          <Link
            className={styles.brand}
            href="/"
          >
            ShortScale
          </Link>

          <nav
            className={styles.nav}
            aria-label="Product navigation"
          >
            <a
              className={styles.navLink}
              href="#recent-links"
            >
              My links
            </a>

            <LogoutButton />
          </nav>
        </header>

        <main className={styles.content}>
          <ProductHome />
        </main>
      </div>
    </ProtectedRoute>
  );
}

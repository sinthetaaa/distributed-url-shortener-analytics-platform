import { AuthForm } from "@/components/auth/auth-form";
import { AuthPageGuard } from "@/components/auth/auth-page-guard";
import { AuthShell } from "@/components/auth/auth-shell";

import styles from "@/components/auth/auth.module.css";

type LoginPageProps = {
  searchParams: Promise<{
    registered?: string;
    reason?: string;
  }>;
};

export default async function LoginPage({
  searchParams,
}: LoginPageProps) {
  const params = await searchParams;

  let notice: string | null = null;

  if (params.registered === "1") {
    notice = "Account created. Sign in to continue.";
  } else if (params.reason === "expired") {
    notice = "Your session expired. Sign in again.";
  } else if (params.reason === "unavailable") {
    notice =
      "Sign in again when ShortScale is available.";
  }

  return (
    <AuthPageGuard>
      <AuthShell
        title="Welcome back"
        description="Sign in to manage your short links and analytics."
        footerPrompt="New to ShortScale?"
        footerLabel="Create an account"
        footerHref="/auth/register"
      >
        {notice ? (
          <p className={styles.notice} role="status">
            {notice}
          </p>
        ) : null}

        <AuthForm mode="login" />
      </AuthShell>
    </AuthPageGuard>
  );
}

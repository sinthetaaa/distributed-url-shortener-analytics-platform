import { AuthForm } from "@/components/auth/auth-form";
import { AuthPageGuard } from "@/components/auth/auth-page-guard";
import { AuthShell } from "@/components/auth/auth-shell";

export default function RegisterPage() {
  return (
    <AuthPageGuard>
      <AuthShell
        title="Create your account"
        description="Create an account to shorten links and keep their analytics private."
        footerPrompt="Already have an account?"
        footerLabel="Sign in"
        footerHref="/auth/login"
      >
        <AuthForm mode="register" />
      </AuthShell>
    </AuthPageGuard>
  );
}

import { AuthForm } from "@/components/auth/auth-form";
import { AuthShell } from "@/components/auth/auth-shell";

export default function LoginPage() {
  return (
    <AuthShell
      title="Welcome back"
      description="Sign in to manage your short links and analytics."
      footerPrompt="New to ShortScale?"
      footerLabel="Create an account"
      footerHref="/auth/register"
    >
      <AuthForm mode="login" />
    </AuthShell>
  );
}

import { LogoutButton } from "@/components/auth/logout-button";
import { ProtectedRoute } from "@/components/auth/protected-route";

export default function Home() {
  return (
    <ProtectedRoute>
      <main className="foundation">
        <div className="foundation__bar">
          <p className="foundation__name">ShortScale</p>
          <LogoutButton />
        </div>
      </main>
    </ProtectedRoute>
  );
}

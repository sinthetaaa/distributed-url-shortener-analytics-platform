import { ProtectedRoute } from "@/components/auth/protected-route";
import { AnalyticsPage } from "@/components/analytics/analytics-page";

type PageProps = {
  params: Promise<{
    shortCode: string;
  }>;
};

export default async function Page({
  params,
}: PageProps) {
  const { shortCode } = await params;

  return (
    <ProtectedRoute>
      <AnalyticsPage
        shortCode={shortCode}
      />
    </ProtectedRoute>
  );
}

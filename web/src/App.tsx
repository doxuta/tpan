import { useEffect, useState } from "react";

type Health = { status: string; version: string; commit: string };

// Trang tạm của khung dự án: chỉ gọi API health để chứng minh
// giao diện và backend nói chuyện được với nhau.
export function App() {
  const [health, setHealth] = useState<Health | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    fetch("/api/v1/health")
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        return res.json() as Promise<Health>;
      })
      .then(setHealth)
      .catch((err: unknown) => setError(String(err)));
  }, []);

  return (
    <main style={{ fontFamily: "system-ui, sans-serif", padding: 32 }}>
      <h1>tpan</h1>
      {health && (
        <p>
          API: {health.status} · phiên bản {health.version} ({health.commit})
        </p>
      )}
      {error && <p>Không gọi được API: {error}</p>}
      {!health && !error && <p>Đang gọi API…</p>}
    </main>
  );
}

import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "FlowLens",
  description:
    "Visualize your team's development process: PR review latency, CI, and lead time.",
};

export default function RootLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="en">
      <body className="min-h-screen antialiased">{children}</body>
    </html>
  );
}

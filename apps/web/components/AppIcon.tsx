export function AppIcon({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 64 64" className={className} aria-hidden="true">
      <rect x="0" y="0" width="64" height="64" rx="14" fill="#132026" />
      <polyline
        points="14,44 32,32 50,20"
        fill="none"
        stroke="#DCE6E4"
        strokeWidth="3"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <circle cx="14" cy="44" r="3" fill="#DCE6E4" />
      <circle cx="50" cy="20" r="3.6" fill="#DCE6E4" />
      <circle cx="32" cy="32" r="11" fill="none" stroke="#5FBFB3" strokeWidth="3" />
      <circle cx="32" cy="32" r="4.6" fill="#5FBFB3" />
    </svg>
  );
}

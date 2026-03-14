interface SectionBadgeProps {
  label: string
}

export default function SectionBadge({ label }: SectionBadgeProps) {
  return (
    <span className="inline-block px-4 py-1.5 bg-accent-lime text-text-dark font-mono text-label uppercase tracking-wider  font-bold">
      {label}
    </span>
  )
}

'use client'

interface BuildProgressProps {
  currentPhase: number
  phases?: string[]
}

const defaultPhases = ['Clone', 'Detect', 'Build', 'Service', 'Done']

export default function BuildProgress({ currentPhase, phases = defaultPhases }: BuildProgressProps) {
  return (
    <div className="flex items-center gap-1 w-full">
      {phases.map((phase, i) => (
        <div key={phase} className="flex items-center flex-1">
          <div className="flex flex-col items-center flex-1">
            <div className="flex items-center w-full">
              <div
                className={`
                  h-1.5 flex-1  transition-all duration-500
                  ${i < currentPhase ? 'bg-accent-lime' : i === currentPhase ? 'bg-accent-lime animate-pulse' : 'bg-border-dark'}
                `}
              />
            </div>
            <span
              className={`
                font-mono text-label uppercase mt-2 transition-colors
                ${i <= currentPhase ? 'text-accent-lime' : 'text-text-secondary'}
                ${i === currentPhase ? 'font-bold' : ''}
              `}
            >
              {phase}
              {i === currentPhase && i < phases.length - 1 && (
                <span className="animate-pulse">...</span>
              )}
            </span>
          </div>
          {i < phases.length - 1 && (
            <div className="w-2" />
          )}
        </div>
      ))}
    </div>
  )
}

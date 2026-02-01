'use client'

export default function PipelineDiagram({ animated = true }: { animated?: boolean }) {
  const nodes = [
    { label: 'Internet', x: 30 },
    { label: 'Cloudflare Edge', x: 200 },
    { label: 'cloudflared', x: 400 },
    { label: 'Nginx', x: 570 },
    { label: 'Your App', x: 720 },
  ]

  return (
    <svg viewBox="0 0 850 100" className="w-full h-auto" xmlns="http://www.w3.org/2000/svg">
      {/* Connection lines */}
      {nodes.slice(0, -1).map((node, i) => {
        const next = nodes[i + 1]
        return (
          <line
            key={`line-${i}`}
            x1={node.x + 60}
            y1={50}
            x2={next.x}
            y2={50}
            stroke="#AAFF45"
            strokeWidth={2}
            strokeDasharray="6 4"
            className={animated ? 'animate-dash' : ''}
            style={{ animationDelay: `${i * 0.2}s` }}
          />
        )
      })}

      {/* Nodes */}
      {nodes.map((node, i) => (
        <g key={i}>
          <rect
            x={node.x}
            y={30}
            width={120}
            height={40}
            rx={8}
            fill={i % 2 === 0 ? '#1C1C1C' : '#F5F5F5'}
            stroke="#AAFF45"
            strokeWidth={1.5}
          />
          <text
            x={node.x + 60}
            y={55}
            textAnchor="middle"
            fill={i % 2 === 0 ? '#FFFFFF' : '#111111'}
            fontSize={11}
            fontFamily="IBM Plex Mono"
            fontWeight={500}
          >
            {node.label}
          </text>
        </g>
      ))}

      {/* Animated dots flowing through */}
      {animated && nodes.slice(0, -1).map((node, i) => {
        const next = nodes[i + 1]
        return (
          <circle
            key={`dot-${i}`}
            r={3}
            fill="#AAFF45"
          >
            <animateMotion
              dur="2s"
              repeatCount="indefinite"
              begin={`${i * 0.4}s`}
              path={`M${node.x + 60},50 L${next.x},50`}
            />
          </circle>
        )
      })}
    </svg>
  )
}

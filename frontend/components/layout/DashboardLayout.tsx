'use client'

import { useState } from 'react'
import Sidebar from '@/components/layout/Sidebar'
import TopStatusBar from '@/components/layout/TopStatusBar'

export default function DashboardLayout({
  children,
}: {
  children: React.ReactNode
}) {
  const [mobileOpen, setMobileOpen] = useState(false)

  return (
    <div className="flex min-h-screen">
      <Sidebar mobileOpen={mobileOpen} setMobileOpen={setMobileOpen} />
      <div className="flex-1 lg:ml-[240px] transition-all duration-300">
        <TopStatusBar onMenuClick={() => setMobileOpen(true)} />
        <main className="p-8">
          {children}
        </main>
      </div>
    </div>
  )
}

/** @type {import('next').NextConfig} */
const nextConfig = {
    output: 'export',
    distDir: '../backend/static/frontend',
    images: {
        unoptimized: true
    },
    typescript: {
        // Don't type check during build - we'll do it separately
        ignoreBuildErrors: true,
    },
    eslint: {
        // Don't run ESLint during build - we'll do it separately
        ignoreDuringBuilds: true,
    }
}

module.exports = nextConfig

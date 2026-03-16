/** @type {import('next').NextConfig} */
const nextConfig = {
    output: 'export',
    distDir: '../backend/static/frontend',
    images: {
        unoptimized: true
    },
    typescript: {
        ignoreBuildErrors: true,
    },
    eslint: {
        ignoreDuringBuilds: true,
    }
}

module.exports = nextConfig

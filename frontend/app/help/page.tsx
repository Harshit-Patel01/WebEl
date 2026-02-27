'use client'

import { motion } from 'framer-motion'
import { BookOpen, Shield, Network, Server, Lock, AlertTriangle, CheckCircle2, ExternalLink } from 'lucide-react'
import SectionBadge from '@/components/ui/SectionBadge'

export default function HelpPage() {
  return (
    <motion.div
      initial={{ opacity: 0, x: 20 }}
      animate={{ opacity: 1, x: 0 }}
      transition={{ duration: 0.3 }}
    >
      <div className="mb-8">
        <SectionBadge label="HELP & DOCUMENTATION" />
      </div>

      <div className="max-w-4xl">
        {/* Introduction */}
        <section className="mb-12">
          <div className="flex items-center gap-3 mb-4">
            <BookOpen className="text-accent-lime" size={24} />
            <h2 className="font-serif text-h2">Getting Started</h2>
          </div>
          <div className="bg-bg-secondary rounded-card border border-border-dark p-6">
            <p className="font-sans text-body text-text-secondary mb-4">
              OpenDeploy is a self-hosted deployment platform that turns your Linux device into a powerful web hosting solution.
              This guide will help you understand how to use OpenDeploy effectively and securely.
            </p>
            <div className="space-y-3 font-mono text-small">
              <div className="flex items-start gap-3">
                <CheckCircle2 size={16} className="text-accent-lime mt-0.5 flex-shrink-0" />
                <span className="text-text-primary">Deploy applications from GitHub repositories</span>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle2 size={16} className="text-accent-lime mt-0.5 flex-shrink-0" />
                <span className="text-text-primary">Automatic builds with Node.js, Python, and static sites</span>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle2 size={16} className="text-accent-lime mt-0.5 flex-shrink-0" />
                <span className="text-text-primary">Cloudflare Tunnel integration for secure internet access</span>
              </div>
              <div className="flex items-start gap-3">
                <CheckCircle2 size={16} className="text-accent-lime mt-0.5 flex-shrink-0" />
                <span className="text-text-primary">Nginx reverse proxy configuration</span>
              </div>
            </div>
          </div>
        </section>

        {/* Security Best Practices */}
        <section className="mb-12">
          <div className="flex items-center gap-3 mb-4">
            <Shield className="text-accent-lime" size={24} />
            <h2 className="font-serif text-h2">Security Best Practices</h2>
          </div>
          <div className="bg-bg-secondary rounded-card border border-border-dark p-6 space-y-6">
            <div>
              <h3 className="font-mono text-small font-bold text-accent-lime mb-3 uppercase tracking-wider">
                1. Change Default Password
              </h3>
              <p className="font-sans text-body text-text-secondary mb-2">
                Immediately change the default dashboard password in Settings → Security. Use a strong, unique password.
              </p>
            </div>

            <div>
              <h3 className="font-mono text-small font-bold text-accent-lime mb-3 uppercase tracking-wider">
                2. Use HTTPS/TLS
              </h3>
              <p className="font-sans text-body text-text-secondary mb-2">
                Always use Cloudflare Tunnel for production deployments. It provides automatic SSL/TLS encryption and never exposes your device directly to the internet.
              </p>
            </div>

            <div>
              <h3 className="font-mono text-small font-bold text-accent-lime mb-3 uppercase tracking-wider">
                3. Environment Variables
              </h3>
              <p className="font-sans text-body text-text-secondary mb-2">
                Mark sensitive environment variables (API keys, database passwords) as "secret" when deploying. These are encrypted at rest.
              </p>
            </div>

            <div>
              <h3 className="font-mono text-small font-bold text-accent-lime mb-3 uppercase tracking-wider">
                4. Regular Updates
              </h3>
              <p className="font-sans text-body text-text-secondary mb-2">
                Keep your system and OpenDeploy updated. The bootable image includes automatic security updates for the operating system.
              </p>
            </div>
          </div>
        </section>

        {/* Step-by-Step Deployment */}
        <section className="mb-12">
          <div className="flex items-center gap-3 mb-4">
            <Server className="text-accent-lime" size={24} />
            <h2 className="font-serif text-h2">Step-by-Step Deployment Walkthrough</h2>
          </div>
          <div className="bg-bg-secondary rounded-card border border-border-dark p-6">
            <div className="space-y-6">
              <div>
                <h4 className="font-mono text-small font-bold text-accent-lime mb-2">Step 1: Prepare Your Repository</h4>
                <p className="font-sans text-small text-text-secondary mb-2">
                  Ensure your GitHub repository is public or you have access credentials. Your project should include a package.json (Node.js), requirements.txt (Python), or be a static site.
                </p>
              </div>

              <div>
                <h4 className="font-mono text-small font-bold text-accent-lime mb-2">Step 2: Navigate to Deploy Page</h4>
                <p className="font-sans text-small text-text-secondary mb-2">
                  Click on "Deploy" in the sidebar. Enter your GitHub repository URL in the format: https://github.com/username/repository
                </p>
              </div>

              <div>
                <h4 className="font-mono text-small font-bold text-accent-lime mb-2">Step 3: Configure Build Settings</h4>
                <p className="font-sans text-small text-text-secondary mb-2">
                  OpenDeploy auto-detects your project type. Verify the build command (e.g., "npm run build") and output directory (e.g., "dist" or "build"). Adjust if needed.
                </p>
              </div>

              <div>
                <h4 className="font-mono text-small font-bold text-accent-lime mb-2">Step 4: Set Environment Variables</h4>
                <p className="font-sans text-small text-text-secondary mb-2">
                  Add any required environment variables. Mark sensitive values like API keys as "secret" to encrypt them. These will be available to your application at runtime.
                </p>
              </div>

              <div>
                <h4 className="font-mono text-small font-bold text-accent-lime mb-2">Step 5: Deploy</h4>
                <p className="font-sans text-small text-text-secondary mb-2">
                  Click "Deploy" and monitor the build logs in real-time. OpenDeploy will clone your repository, install dependencies, run the build command, and start your application.
                </p>
              </div>

              <div>
                <h4 className="font-mono text-small font-bold text-accent-lime mb-2">Step 6: Configure Internet Access</h4>
                <p className="font-sans text-small text-text-secondary mb-2">
                  Once deployed, go to the Tunnel section to expose your application to the internet via Cloudflare Tunnel. This provides secure HTTPS access without port forwarding.
                </p>
              </div>
            </div>
          </div>
        </section>

        {/* Environment Variables Best Practices */}
        <section className="mb-12">
          <div className="flex items-center gap-3 mb-4">
            <Lock className="text-accent-lime" size={24} />
            <h2 className="font-serif text-h2">Environment Variables Best Practices</h2>
          </div>
          <div className="bg-bg-secondary rounded-card border border-border-dark p-6 space-y-6">
            <div>
              <h3 className="font-mono text-small font-bold text-accent-lime mb-3 uppercase tracking-wider">
                What Are Environment Variables?
              </h3>
              <p className="font-sans text-body text-text-secondary mb-2">
                Environment variables are key-value pairs that configure your application without hardcoding sensitive data. They're essential for API keys, database credentials, and configuration settings.
              </p>
            </div>

            <div>
              <h3 className="font-mono text-small font-bold text-accent-lime mb-3 uppercase tracking-wider">
                Marking Variables as Secret
              </h3>
              <p className="font-sans text-body text-text-secondary mb-2">
                Always mark sensitive variables as "secret" in the deployment form. Secret variables are encrypted at rest and never displayed in logs or the UI after being set.
              </p>
              <div className="mt-3 space-y-2">
                <div className="flex items-start gap-3">
                  <CheckCircle2 size={16} className="text-accent-lime mt-0.5 flex-shrink-0" />
                  <span className="font-sans text-small text-text-secondary">API keys and tokens</span>
                </div>
                <div className="flex items-start gap-3">
                  <CheckCircle2 size={16} className="text-accent-lime mt-0.5 flex-shrink-0" />
                  <span className="font-sans text-small text-text-secondary">Database passwords</span>
                </div>
                <div className="flex items-start gap-3">
                  <CheckCircle2 size={16} className="text-accent-lime mt-0.5 flex-shrink-0" />
                  <span className="font-sans text-small text-text-secondary">OAuth client secrets</span>
                </div>
                <div className="flex items-start gap-3">
                  <CheckCircle2 size={16} className="text-accent-lime mt-0.5 flex-shrink-0" />
                  <span className="font-sans text-small text-text-secondary">Encryption keys</span>
                </div>
              </div>
            </div>

            <div>
              <h3 className="font-mono text-small font-bold text-accent-lime mb-3 uppercase tracking-wider">
                Naming Conventions
              </h3>
              <p className="font-sans text-body text-text-secondary mb-2">
                Use UPPERCASE_WITH_UNDERSCORES for environment variable names. This makes them easily identifiable in your code and follows industry standards.
              </p>
              <div className="mt-3 bg-bg-primary rounded-lg p-4 font-mono text-[11px] text-text-primary border border-border-dark">
                <div className="text-accent-lime">✓ Good:</div>
                <div className="ml-4 mt-1">DATABASE_URL</div>
                <div className="ml-4">API_KEY</div>
                <div className="ml-4">STRIPE_SECRET_KEY</div>
                <div className="text-status-error mt-3">✗ Avoid:</div>
                <div className="ml-4 mt-1">databaseUrl</div>
                <div className="ml-4">api-key</div>
                <div className="ml-4">stripeSecretKey</div>
              </div>
            </div>

            <div>
              <h3 className="font-mono text-small font-bold text-accent-lime mb-3 uppercase tracking-wider">
                Common Environment Variables
              </h3>
              <div className="space-y-2 font-mono text-small">
                <div className="flex items-start gap-3">
                  <span className="text-accent-lime">NODE_ENV</span>
                  <span className="text-text-secondary">- Set to "production" for deployed apps</span>
                </div>
                <div className="flex items-start gap-3">
                  <span className="text-accent-lime">PORT</span>
                  <span className="text-text-secondary">- The port your application listens on</span>
                </div>
                <div className="flex items-start gap-3">
                  <span className="text-accent-lime">DATABASE_URL</span>
                  <span className="text-text-secondary">- Database connection string</span>
                </div>
                <div className="flex items-start gap-3">
                  <span className="text-accent-lime">API_BASE_URL</span>
                  <span className="text-text-secondary">- Base URL for API endpoints</span>
                </div>
              </div>
            </div>

            <div>
              <h3 className="font-mono text-small font-bold text-accent-lime mb-3 uppercase tracking-wider">
                Never Commit Secrets
              </h3>
              <p className="font-sans text-body text-text-secondary mb-2">
                Never commit .env files or hardcode secrets in your repository. Use OpenDeploy's environment variable system to inject secrets at deployment time.
              </p>
            </div>
          </div>
        </section>

        {/* GitHub Repository Setup */}
        <section className="mb-12">
          <div className="flex items-center gap-3 mb-4">
            <BookOpen className="text-accent-lime" size={24} />
            <h2 className="font-serif text-h2">GitHub Repository Setup Guide</h2>
          </div>
          <div className="bg-bg-secondary rounded-card border border-border-dark p-6 space-y-6">
            <div>
              <h3 className="font-mono text-small font-bold text-accent-lime mb-3 uppercase tracking-wider">
                Repository Requirements
              </h3>
              <p className="font-sans text-body text-text-secondary mb-2">
                Your repository must be accessible (public or with credentials) and contain the necessary configuration files for your project type.
              </p>
            </div>

            <div>
              <h3 className="font-mono text-small font-bold text-accent-lime mb-3 uppercase tracking-wider">
                Node.js Projects
              </h3>
              <p className="font-sans text-body text-text-secondary mb-2">
                Ensure your package.json includes a build script and all dependencies:
              </p>
              <div className="mt-3 bg-bg-primary rounded-lg p-4 font-mono text-[11px] text-text-primary border border-border-dark">
                <pre>{`{
  "name": "my-app",
  "scripts": {
    "build": "next build",
    "start": "next start"
  },
  "dependencies": {
    "next": "^14.0.0",
    "react": "^18.0.0"
  }
}`}</pre>
              </div>
            </div>

            <div>
              <h3 className="font-mono text-small font-bold text-accent-lime mb-3 uppercase tracking-wider">
                Python Projects
              </h3>
              <p className="font-sans text-body text-text-secondary mb-2">
                Include a requirements.txt file with all dependencies:
              </p>
              <div className="mt-3 bg-bg-primary rounded-lg p-4 font-mono text-[11px] text-text-primary border border-border-dark">
                <pre>{`flask==3.0.0
gunicorn==21.2.0
python-dotenv==1.0.0`}</pre>
              </div>
            </div>

            <div>
              <h3 className="font-mono text-small font-bold text-accent-lime mb-3 uppercase tracking-wider">
                Static Sites
              </h3>
              <p className="font-sans text-body text-text-secondary mb-2">
                For static sites, ensure your build output goes to a standard directory like "dist", "build", or "public". OpenDeploy will serve these files directly.
              </p>
            </div>

            <div>
              <h3 className="font-mono text-small font-bold text-accent-lime mb-3 uppercase tracking-wider">
                Private Repositories
              </h3>
              <p className="font-sans text-body text-text-secondary mb-2">
                For private repositories, you'll need to provide a GitHub personal access token with repo access. Generate one in GitHub Settings → Developer settings → Personal access tokens.
              </p>
            </div>
          </div>
        </section>

        {/* Cloudflare Tunnel */}
        <section className="mb-12">
          <div className="flex items-center gap-3 mb-4">
            <Network className="text-accent-lime" size={24} />
            <h2 className="font-serif text-h2">Cloudflare Tunnel (Recommended)</h2>
          </div>
          <div className="bg-bg-secondary rounded-card border border-border-dark p-6">
            <p className="font-sans text-body text-text-secondary mb-4">
              Cloudflare Tunnel provides secure access to your applications without exposing ports or your IP address.
            </p>

            <div className="space-y-4">
              <div>
                <h4 className="font-mono text-small font-bold text-accent-lime mb-2">Benefits</h4>
                <ul className="font-sans text-small text-text-secondary space-y-2 ml-4">
                  <li>• No port forwarding required</li>
                  <li>• DDoS protection included</li>
                  <li>• Free SSL/TLS certificates</li>
                  <li>• Hide your home IP address</li>
                  <li>• Built-in firewall and access controls</li>
                </ul>
              </div>

              <div>
                <h4 className="font-mono text-small font-bold text-text-primary mb-2">Setup Steps</h4>
                <ol className="font-sans text-small text-text-secondary space-y-2 ml-4 list-decimal">
                  <li>Create a free Cloudflare account at cloudflare.com</li>
                  <li>Add your domain to Cloudflare and update nameservers</li>
                  <li>Go to Tunnel → Dashboard in OpenDeploy</li>
                  <li>Enter your Cloudflare API token (create in Cloudflare dashboard)</li>
                  <li>Create a tunnel and configure routes to your deployed applications</li>
                  <li>Your application is now accessible via your domain with automatic HTTPS</li>
                </ol>
              </div>
            </div>
          </div>
        </section>

        {/* Troubleshooting */}
        <section className="mb-12">
          <div className="flex items-center gap-3 mb-4">
            <AlertTriangle className="text-accent-lime" size={24} />
            <h2 className="font-serif text-h2">Troubleshooting Common Issues</h2>
          </div>
          <div className="bg-bg-secondary rounded-card border border-border-dark p-6 space-y-6">
            <div>
              <h4 className="font-mono text-small font-bold text-text-primary mb-2">Build Fails</h4>
              <p className="font-sans text-small text-text-secondary mb-2">
                Check the build logs for errors. Ensure your build command and output directory are correct. Verify all dependencies are listed in package.json or requirements.txt.
              </p>
              <div className="mt-2 ml-4 space-y-1 font-sans text-small text-text-secondary">
                <div>• Verify Node.js/Python version compatibility</div>
                <div>• Check for missing dependencies in package.json</div>
                <div>• Ensure build scripts are defined correctly</div>
                <div>• Look for syntax errors in your code</div>
              </div>
            </div>

            <div>
              <h4 className="font-mono text-small font-bold text-text-primary mb-2">Service Won't Start</h4>
              <p className="font-sans text-small text-text-secondary mb-2">
                Check service logs in the Logs page. Ensure the port isn't already in use. Verify environment variables are set correctly.
              </p>
              <div className="mt-2 ml-4 space-y-1 font-sans text-small text-text-secondary">
                <div>• Check if another service is using the same port</div>
                <div>• Verify all required environment variables are set</div>
                <div>• Review application logs for startup errors</div>
                <div>• Ensure file permissions are correct</div>
              </div>
            </div>

            <div>
              <h4 className="font-mono text-small font-bold text-text-primary mb-2">Application Returns 502 Bad Gateway</h4>
              <p className="font-sans text-small text-text-secondary mb-2">
                This usually means your application isn't running or isn't listening on the correct port. Check application logs and verify the port configuration.
              </p>
              <div className="mt-2 ml-4 space-y-1 font-sans text-small text-text-secondary">
                <div>• Verify application is running (check Logs page)</div>
                <div>• Ensure application listens on the configured port</div>
                <div>• Check Nginx proxy configuration</div>
                <div>• Review application startup logs for errors</div>
              </div>
            </div>

            <div>
              <h4 className="font-mono text-small font-bold text-text-primary mb-2">Out of Disk Space</h4>
              <p className="font-sans text-small text-text-secondary mb-2">
                Monitor disk usage in the Dashboard. Remove old deployments or logs to free up space. Consider using an external storage device.
              </p>
              <div className="mt-2 ml-4 space-y-1 font-sans text-small text-text-secondary">
                <div>• Delete unused deployments from Deploy page</div>
                <div>• Clear old log files</div>
                <div>• Remove Docker images if using containers</div>
                <div>• Check for large temporary files</div>
              </div>
            </div>

            <div>
              <h4 className="font-mono text-small font-bold text-text-primary mb-2">GitHub Clone Fails</h4>
              <p className="font-sans text-small text-text-secondary mb-2">
                Verify the repository URL is correct and accessible. For private repos, ensure your access token has the necessary permissions.
              </p>
              <div className="mt-2 ml-4 space-y-1 font-sans text-small text-text-secondary">
                <div>• Check repository URL format (https://github.com/user/repo)</div>
                <div>• Verify repository is public or token has access</div>
                <div>• Ensure GitHub personal access token hasn't expired</div>
                <div>• Check network connectivity to GitHub</div>
              </div>
            </div>
          </div>
        </section>

        {/* Additional Resources */}
        <section className="mb-12">
          <div className="flex items-center gap-3 mb-4">
            <ExternalLink className="text-accent-lime" size={24} />
            <h2 className="font-serif text-h2">Additional Resources</h2>
          </div>
          <div className="bg-bg-secondary rounded-card border border-border-dark p-6">
            <div className="space-y-3 font-mono text-small">
              <a href="https://nginx.org/en/docs/" target="_blank" rel="noopener noreferrer" className="flex items-center gap-2 text-accent-lime hover:text-accent-lime-muted transition-colors">
                <ExternalLink size={14} /> Nginx Documentation
              </a>
              <a href="https://developers.cloudflare.com/cloudflare-one/connections/connect-apps/" target="_blank" rel="noopener noreferrer" className="flex items-center gap-2 text-accent-lime hover:text-accent-lime-muted transition-colors">
                <ExternalLink size={14} /> Cloudflare Tunnel Docs
              </a>
              <a href="https://letsencrypt.org/getting-started/" target="_blank" rel="noopener noreferrer" className="flex items-center gap-2 text-accent-lime hover:text-accent-lime-muted transition-colors">
                <ExternalLink size={14} /> Let's Encrypt Guide
              </a>
              <a href="https://owasp.org/www-project-top-ten/" target="_blank" rel="noopener noreferrer" className="flex items-center gap-2 text-accent-lime hover:text-accent-lime-muted transition-colors">
                <ExternalLink size={14} /> OWASP Top 10 Security Risks
              </a>
            </div>
          </div>
        </section>
      </div>
    </motion.div>
  )
}

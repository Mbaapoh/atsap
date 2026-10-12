// Jenkins pipeline for the atsap project.
//
// This file is one implementation of the provider-agnostic CI contract
// (docs/TOOLSET.md §5: `mise run ci`, image build/scan/publish, test
// reporting, main-only push+deploy) — not the contract itself. Any CI
// provider implementing the same stages in the same order satisfies it.

// Requires on the agent: Docker (with socket access) and curl. Go,
// golangci-lint, Node, osv-scanner, gitleaks and trivy are NOT required on
// the agent — they're installed by mise from .mise.toml and
// console/mise.toml, so CI uses the exact same tool versions as local dev.
//
// Jenkins plugins used for reports: JUnit, Coverage (Cobertura), and
// Warnings Next Generation (SARIF). Every gate also fails the build by its
// exit code alone, so the plugins only render; they never decide.
//
// Expected Jenkins configuration:
//   - Multibranch pipeline (so `branch 'main'` gating below works), or
//     adjust the `when` blocks to your branching model.
//   - Credentials:
//       docker-registry-creds  (username/password) - push access to REGISTRY
//       atsap-deploy-ssh-key   (SSH private key)    - access to the deploy host
//   - Job/global environment variables (or edit the defaults below):
//       REGISTRY      e.g. registry.example.com/atsap
//       DEPLOY_HOST   e.g. atsap.example.com
//       DEPLOY_USER   e.g. deploy
//       DEPLOY_PATH   absolute path to this repo checkout on the deploy host

pipeline {
    agent { label 'docker' }

    options {
        timestamps()
        disableConcurrentBuilds()
        buildDiscarder(logRotator(numToKeepStr: '20'))
    }

    environment {
        MISE_BIN            = "${HOME}/.local/bin/mise"
        REGISTRY            = "${env.REGISTRY ?: 'registry.example.com/atsap'}"
        IMAGE_TAG           = "${env.GIT_COMMIT.take(7)}"
        GOVULNCHECK_VERSION = "${env.GOVULNCHECK_VERSION ?: 'v1.7.0'}"
        DEPLOY_HOST         = "${env.DEPLOY_HOST ?: ''}"
        DEPLOY_USER         = "${env.DEPLOY_USER ?: 'deploy'}"
        DEPLOY_PATH         = "${env.DEPLOY_PATH ?: '/opt/atsap'}"
    }

    stages {
        stage('Setup') {
            steps {
                sh '''
                    set -eu
                    if [ ! -x "$MISE_BIN" ]; then
                        curl -fsSL https://mise.run | sh
                    fi
                    "$MISE_BIN" trust
                    "$MISE_BIN" install
                    (cd console && "$MISE_BIN" trust && "$MISE_BIN" install)
                '''
            }
        }

        stage('Lint') {
            steps {
                sh 'cd api && "$MISE_BIN" x -- golangci-lint run ./...'
            }
        }

        stage('Test') {
            steps {
                // Unit tests with coverage, gated against the "unit" floors
                // in quality/coverage-floors.json (D-71). JUnit and
                // Cobertura are published below: per-build history and
                // trends live in CI, never as committed files (D-28).
                sh '"$MISE_BIN" x -- make coverage'
            }
            post {
                always {
                    junit allowEmptyResults: true, testResults: 'coverage/go/unit-junit.xml'
                    recordCoverage tools: [[parser: 'COBERTURA', pattern: 'coverage/go/cobertura.xml']], id: 'go', name: 'Go coverage'
                    archiveArtifacts artifacts: 'coverage/go/merged.out', allowEmptyArchive: true
                }
            }
        }

        stage('Console') {
            steps {
                // The console's hermetic tiers (docs/TOOLSET.md §3a):
                // typecheck, component tests with coverage floors, browser
                // e2e with the API mocked, and the production dependency
                // audit. Its integration and UAT tiers need the dev rig and
                // run beside the Go e2e suites, not here.
                sh '''
                    set -eu
                    "$MISE_BIN" x -- make console-install
                    "$MISE_BIN" x -- make console-test
                    (cd console && "$MISE_BIN" x -- npx playwright install chromium)
                    "$MISE_BIN" x -- make console-e2e
                    "$MISE_BIN" x -- make console-audit
                '''
            }
            post {
                always {
                    recordCoverage tools: [[parser: 'COBERTURA', pattern: 'console/coverage/cobertura-coverage.xml']], id: 'console', name: 'Console coverage'
                }
            }
        }

        stage('Docs') {
            steps {
                // HLD markers, decision-citation resolution, and internal
                // .md link integrity (docs/hld/README.md §5.1; D-28).
                sh './scripts/check-docs.sh'
            }
        }

        stage('Security: supply chain') {
            steps {
                // D-35, D-71: OSV-Scanner over both lockfiles (known
                // vulnerabilities and the permissive-licence allowlist),
                // govulncheck (call-reachability for Go), and gitleaks over
                // the working tree. Any finding fails the build.
                sh '''
                    set -eu
                    "$MISE_BIN" x -- make sca
                    "$MISE_BIN" x -- make secrets
                '''
            }
            post {
                always {
                    recordIssues tools: [sarif(pattern: 'quality-reports/*.sarif')], qualityGates: [[threshold: 1, type: 'TOTAL', unstable: false]]
                    archiveArtifacts artifacts: 'quality-reports/*.sarif', allowEmptyArchive: true
                }
            }
        }

        stage('Build images') {
            steps {
                sh """
                    docker build -t ${REGISTRY}/atsap-api:${IMAGE_TAG} api
                    docker build -t ${REGISTRY}/atsap-core:${IMAGE_TAG} core
                """
            }
        }

        stage('Security: container scan') {
            steps {
                // Trivy, pinned in .mise.toml (TOOLSET §1.3; D-35, D-71):
                // HIGH/CRITICAL vulnerabilities fail the build before any
                // push or deploy, and each image's CycloneDX SBOM is
                // archived with the build.
                sh '''
                    set -eu
                    export TRIVY_DB_REPOSITORY="ghcr.io/aquasecurity/trivy-db:2,mirror.gcr.io/aquasec/trivy-db:2"
                    mkdir -p quality-reports
                    for name in atsap-api atsap-core; do
                        img="${REGISTRY}/${name}:${IMAGE_TAG}"
                        "$MISE_BIN" x -- trivy image --quiet --timeout 20m --scanners vuln --exit-code 1 --severity HIGH,CRITICAL "$img"
                        "$MISE_BIN" x -- trivy image --quiet --timeout 20m --format cyclonedx --output "quality-reports/sbom-${name}.cdx.json" "$img"
                    done
                '''
            }
            post {
                always {
                    archiveArtifacts artifacts: 'quality-reports/sbom-*.cdx.json', allowEmptyArchive: true
                }
            }
        }

        stage('Push images') {
            when { branch 'main' }
            steps {
                withCredentials([usernamePassword(credentialsId: 'docker-registry-creds', usernameVariable: 'REG_USER', passwordVariable: 'REG_PASS')]) {
                    sh """
                        echo "\$REG_PASS" | docker login ${REGISTRY} -u "\$REG_USER" --password-stdin
                        docker tag ${REGISTRY}/atsap-api:${IMAGE_TAG} ${REGISTRY}/atsap-api:latest
                        docker tag ${REGISTRY}/atsap-core:${IMAGE_TAG} ${REGISTRY}/atsap-core:latest
                        docker push ${REGISTRY}/atsap-api:${IMAGE_TAG}
                        docker push ${REGISTRY}/atsap-api:latest
                        docker push ${REGISTRY}/atsap-core:${IMAGE_TAG}
                        docker push ${REGISTRY}/atsap-core:latest
                    """
                }
            }
        }

        stage('Deploy') {
            when { branch 'main' }
            steps {
                sshagent(credentials: ['atsap-deploy-ssh-key']) {
                    sh """
                        ssh -o StrictHostKeyChecking=no ${DEPLOY_USER}@${DEPLOY_HOST} '
                            cd ${DEPLOY_PATH} &&
                            git pull --ff-only &&
                            REGISTRY=${REGISTRY} IMAGE_TAG=${IMAGE_TAG} docker compose -f deploy/docker-compose.prod.yml pull &&
                            REGISTRY=${REGISTRY} IMAGE_TAG=${IMAGE_TAG} docker compose -f deploy/docker-compose.prod.yml up -d
                        '
                    """
                }
            }
        }
    }

    post {
        always {
            sh 'docker image prune -f --filter "until=72h" || true'
            cleanWs()
        }
    }
}

// Jenkins pipeline for the atsap project.
//
// This file is one implementation of the provider-agnostic CI contract
// (docs/TOOLSET.md §5: `mise run ci`, image build/scan/publish, test
// reporting, main-only push+deploy) — not the contract itself. Any CI
// provider implementing the same stages in the same order satisfies it.

// Requires on the agent: Docker (with socket access) and curl. Go and
// golangci-lint are NOT required on the agent — they're installed by mise
// from .mise.toml, so CI uses the exact same tool versions as local dev.
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
                // JUnit XML + coverage profile are published below (stage
                // post): per-build test history and trends live in CI,
                // never as committed files (D-28).
                sh '''
                    mkdir -p test-results coverage
                    cd api && "$MISE_BIN" x -- gotestsum --junitfile ../test-results/unit.xml -- -race -coverprofile=../coverage/unit.out ./...
                '''
            }
            post {
                always {
                    junit 'test-results/unit.xml'
                    archiveArtifacts artifacts: 'coverage/unit.out', allowEmptyArchive: true
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

        stage('Security: Go vulnerabilities') {
            steps {
                // govulncheck pinned (TOOLSET §1.3; D-35): known CVEs fail
                // the build. Version pinned to GOVULNCHECK_VERSION, never @latest.
                sh 'cd api && "$MISE_BIN" x -- go run golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION} ./...'
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
                // Trivy (TOOLSET §1.3; D-35): HIGH/CRITICAL vulnerabilities
                // fail the build before any push/deploy.
                sh '''
                    set -eu
                    for img in "${REGISTRY}/atsap-api:${IMAGE_TAG}" "${REGISTRY}/atsap-core:${IMAGE_TAG}"; do
                        docker run --rm \
                            -v /var/run/docker.sock:/var/run/docker.sock \
                            aquasec/trivy:0.52.2 image --quiet --exit-code 1 --severity HIGH,CRITICAL "$img"
                    done
                '''
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

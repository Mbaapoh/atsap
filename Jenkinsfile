// Jenkins pipeline for the atsap project.
//
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
        MISE_BIN     = "${HOME}/.local/bin/mise"
        REGISTRY     = "${env.REGISTRY ?: 'registry.example.com/atsap'}"
        IMAGE_TAG    = "${env.GIT_COMMIT.take(7)}"
        DEPLOY_HOST  = "${env.DEPLOY_HOST ?: ''}"
        DEPLOY_USER  = "${env.DEPLOY_USER ?: 'deploy'}"
        DEPLOY_PATH  = "${env.DEPLOY_PATH ?: '/opt/atsap'}"
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
                sh 'cd api && "$MISE_BIN" x -- go test ./... -race -cover'
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

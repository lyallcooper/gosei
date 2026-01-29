// Gosei - Application JavaScript

(function() {
    'use strict';

    // ============================================
    // Toast Notifications
    // ============================================
    const Toast = {
        container: null,

        init() {
            this.container = document.getElementById('toast-container');
        },

        show(message, type = 'info', duration = 5000) {
            if (!this.container) return;

            const toast = document.createElement('div');
            toast.className = `toast toast-${type}`;
            toast.textContent = message;

            this.container.appendChild(toast);

            // Auto-remove after duration
            setTimeout(() => {
                toast.style.animation = 'slideIn 0.3s ease reverse';
                setTimeout(() => toast.remove(), 300);
            }, duration);
        },

        success(message) {
            this.show(message, 'success');
        },

        error(message) {
            this.show(message, 'error');
        }
    };

    // ============================================
    // SSE Event Handling
    // ============================================
    const SSE = {
        source: null,
        reconnectAttempts: 0,
        maxReconnectAttempts: 10,
        reconnectDelay: 1000,

        connect() {
            if (this.source) {
                this.source.close();
            }

            this.source = new EventSource('/api/events');

            this.source.onopen = () => {
                console.log('SSE connected');
                this.reconnectAttempts = 0;
                this.updateConnectionStatus(true);
            };

            this.source.onerror = (e) => {
                console.error('SSE error:', e);
                this.updateConnectionStatus(false);
                this.source.close();
                this.reconnect();
            };

            // Handle events
            this.source.addEventListener('connected', (e) => {
                console.log('SSE client ID:', JSON.parse(e.data).clientId);
            });

            this.source.addEventListener('container:status', (e) => {
                const data = JSON.parse(e.data);
                this.handleContainerStatus(data);
            });

            this.source.addEventListener('project:status', (e) => {
                const data = JSON.parse(e.data);
                this.handleProjectStatus(data);
            });

            this.source.addEventListener('compose:output', (e) => {
                const data = JSON.parse(e.data);
                this.handleComposeOutput(data);
            });

            this.source.addEventListener('compose:complete', (e) => {
                const data = JSON.parse(e.data);
                this.handleComposeComplete(data);
            });

            this.source.addEventListener('log', (e) => {
                const data = JSON.parse(e.data);
                this.handleLogLine(data);
            });
        },

        reconnect() {
            if (this.reconnectAttempts >= this.maxReconnectAttempts) {
                console.error('Max reconnect attempts reached');
                Toast.error('Connection lost. Please refresh the page.');
                return;
            }

            this.reconnectAttempts++;
            const delay = this.reconnectDelay * Math.pow(2, this.reconnectAttempts - 1);
            console.log(`Reconnecting in ${delay}ms (attempt ${this.reconnectAttempts})`);

            setTimeout(() => this.connect(), delay);
        },

        updateConnectionStatus(connected) {
            const statusDot = document.querySelector('.status-dot');
            const statusText = document.querySelector('.status-text');

            if (statusDot) {
                statusDot.classList.toggle('connected', connected);
            }
            if (statusText) {
                statusText.textContent = connected ? 'Connected' : 'Disconnected';
            }
        },

        handleContainerStatus(data) {
            // Update container row in tables
            const rows = document.querySelectorAll(`[data-container-id="${data.id}"]`);
            rows.forEach(row => {
                const statusBadge = row.querySelector('.state-badge');
                if (statusBadge) {
                    statusBadge.className = `state-badge state-${data.state}`;
                    statusBadge.innerHTML = `${this.getStatusIcon(data.state)} ${data.state}`;
                }
            });

            // Refresh the project list if on dashboard
            if (document.querySelector('.projects-grid')) {
                htmx.ajax('GET', '/partials/projects', {
                    target: '#projects-container',
                    swap: 'innerHTML'
                });
            }
        },

        handleProjectStatus(data) {
            const card = document.querySelector(`[data-project-id="${data.id}"]`);
            if (card) {
                const statusBadge = card.querySelector('.status-badge');
                if (statusBadge) {
                    statusBadge.className = `status-badge status-${data.status}`;
                    statusBadge.innerHTML = `${this.getStatusIcon(data.status)} ${data.status}`;
                }

                const infoValue = card.querySelector('.info-value');
                if (infoValue) {
                    infoValue.textContent = `${data.running}/${data.total}`;
                }
            }
        },

        handleComposeOutput(data) {
            const modal = document.getElementById('output-modal');
            const outputLog = document.getElementById('output-log');

            if (modal && modal.style.display === 'none') {
                if (outputLog) outputLog.innerHTML = '';
                modal.style.display = 'flex';
            }

            if (outputLog) {
                const line = document.createElement('div');
                line.className = `output-line ${data.stream}`;
                line.textContent = data.line;
                outputLog.appendChild(line);
                outputLog.scrollTop = outputLog.scrollHeight;
            }
        },

        handleComposeComplete(data) {
            if (data.success) {
                Toast.success(`${data.operation} completed successfully`);
            } else {
                Toast.error(`${data.operation} failed: ${data.message}`);
            }

            // Refresh projects list
            if (document.querySelector('.projects-grid')) {
                htmx.ajax('GET', '/partials/projects', {
                    target: '#projects-container',
                    swap: 'innerHTML'
                });
            }

            // Refresh project detail if on that page
            const projectPage = document.querySelector('.project-page');
            if (projectPage) {
                const projectId = window.location.pathname.split('/').pop();
                htmx.ajax('GET', `/partials/projects/${projectId}`, {
                    target: '.containers-section',
                    swap: 'innerHTML'
                });
            }
        },

        handleLogLine(data) {
            const logsContent = document.querySelector('.logs-content');
            if (logsContent) {
                const line = document.createElement('div');
                line.className = 'log-line';

                const timestamp = new Date(data.timestamp);
                const timeStr = timestamp.toTimeString().split(' ')[0];

                line.innerHTML = `
                    <span class="log-timestamp">${timeStr}</span>
                    <span class="log-message">${this.escapeHtml(data.line)}</span>
                `;

                logsContent.appendChild(line);
                logsContent.scrollTop = logsContent.scrollHeight;

                // Limit log lines to prevent memory issues
                const maxLines = 1000;
                while (logsContent.children.length > maxLines) {
                    logsContent.removeChild(logsContent.firstChild);
                }
            }
        },

        getStatusIcon(status) {
            switch (status) {
                case 'running': return '●';
                case 'partial': return '◐';
                case 'stopped': return '○';
                case 'exited': return '○';
                default: return '?';
            }
        },

        escapeHtml(text) {
            const div = document.createElement('div');
            div.textContent = text;
            return div.innerHTML;
        }
    };

    // ============================================
    // Stats Polling
    // ============================================
    const Stats = {
        interval: null,

        start() {
            // Poll stats every 5 seconds for visible containers
            this.interval = setInterval(() => this.poll(), 5000);
            this.poll(); // Initial poll
        },

        stop() {
            if (this.interval) {
                clearInterval(this.interval);
                this.interval = null;
            }
        },

        async poll() {
            const statsCells = document.querySelectorAll('[data-stats-id]');
            if (statsCells.length === 0) return;

            for (const cell of statsCells) {
                const containerId = cell.dataset.statsId;
                try {
                    const response = await fetch(`/api/containers/${containerId}/stats`);
                    if (response.ok) {
                        const stats = await response.json();
                        cell.innerHTML = `${this.formatPercent(stats.cpuPercent)} / ${this.formatBytes(stats.memoryUsage)}`;
                    }
                } catch (e) {
                    // Ignore errors, container might be stopped
                }
            }
        },

        formatPercent(percent) {
            if (percent < 10) return percent.toFixed(1) + '%';
            return Math.round(percent) + '%';
        },

        formatBytes(bytes) {
            if (bytes === 0) return '0 B';
            const k = 1024;
            const sizes = ['B', 'KB', 'MB', 'GB'];
            const i = Math.floor(Math.log(bytes) / Math.log(k));
            return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
        }
    };

    // ============================================
    // HTMX Event Handlers
    // ============================================
    document.body.addEventListener('htmx:responseError', function(event) {
        let message = 'An error occurred';
        try {
            const response = JSON.parse(event.detail.xhr.responseText);
            if (response.error) {
                message = response.error;
            }
        } catch (e) {
            // Ignore parse errors
        }
        Toast.error(message);
    });

    // ============================================
    // Modal
    // ============================================
    function closeOutputModal() {
        const modal = document.getElementById('output-modal');
        if (modal) {
            modal.style.display = 'none';
        }
    }

    window.closeOutputModal = closeOutputModal;

    // ============================================
    // Initialize
    // ============================================
    document.addEventListener('DOMContentLoaded', function() {
        Toast.init();
        SSE.connect();

        if (document.querySelector('[data-stats-id]')) {
            Stats.start();
        }

        document.addEventListener('keydown', function(e) {
            if (e.key === 'Escape') {
                closeOutputModal();
            }
        });
    });

    // Clean up on page unload
    window.addEventListener('beforeunload', function() {
        if (SSE.source) {
            SSE.source.close();
        }
        Stats.stop();
    });

})();

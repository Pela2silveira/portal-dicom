      function detachNode(node) {
        if (node && node.parentNode) {
          node.parentNode.removeChild(node);
        }
      }

      function mountNodeAfter(anchor, node) {
        if (!anchor || !anchor.parentNode || !node) {
          return;
        }
        anchor.parentNode.insertBefore(node, anchor.nextSibling);
      }

      const hero = document.querySelector(".hero");
      const shell = document.querySelector(".shell");
      const authBody = document.querySelector(".auth-body");
      const patientWorkspace = document.getElementById("patient-workspace");
      const physicianWorkspace = document.getElementById("physician-workspace");
      const patientFlow = document.querySelector('[data-flow="patient"]');
      const physicianFlow = document.querySelector('[data-flow="physician"]');
      const roleButtons = document.querySelectorAll("[data-role]");
      const demoRibbons = document.querySelectorAll("[data-demo-ribbon]");
      const mailCodeButton = document.getElementById("send-mail-code");
      const patientDocument = document.getElementById("patient-document");
      const patientMailCode = document.getElementById("patient-mail-code");
      const patientDocumentError = document.getElementById("patient-document-error");
      const patientMailCodeError = document.getElementById("patient-mail-code-error");
      const patientFilterPeriod = document.getElementById("patient-filter-period");
      const patientFilterModality = document.getElementById("patient-filter-modality");
      const patientDateDropdown = document.getElementById("patient-date-dropdown");
      const patientDateSummary = document.getElementById("patient-date-summary");
      const patientCalendarMonthLabel = document.getElementById("patient-calendar-month-label");
      const patientCalendarGrid = document.getElementById("patient-calendar-grid");
      const patientCalendarPrev = document.getElementById("patient-calendar-prev");
      const patientCalendarNext = document.getElementById("patient-calendar-next");
      const patientApplyFiltersButton = document.getElementById("patient-apply-filters");
      const patientSyncStatus = document.getElementById("patient-sync-status");
      const patientStudyList = document.getElementById("patient-study-list");
      const patientFullNameValue = document.getElementById("patient-full-name-value");
      const patientDocumentValue = document.getElementById("patient-document-value");
      const patientBirthDateValue = document.getElementById("patient-birth-date-value");
      const patientSexValue = document.getElementById("patient-sex-value");
      const patientGenderIdentityValue = document.getElementById("patient-gender-identity-value");
      const patientShareQROverlay = document.getElementById("patient-share-qr-overlay");
      const patientShareQRImage = document.getElementById("patient-share-qr-image");
      const patientShareQRExpiresAt = document.getElementById("patient-share-qr-expires-at");
      const patientShareQRMaxUses = document.getElementById("patient-share-qr-max-uses");
      const patientShareQRLink = document.getElementById("patient-share-qr-link");
      const patientShareQRShare = document.getElementById("patient-share-qr-share");
      const patientShareQRCopy = document.getElementById("patient-share-qr-copy");
      const patientShareQRWhatsApp = document.getElementById("patient-share-qr-whatsapp");
      const patientShareQRClose = document.getElementById("patient-share-qr-close");
      const patientPreviewOverlay = document.getElementById("patient-preview-overlay");
      const patientPreviewSummary = document.getElementById("patient-preview-summary");
      const patientPreviewGrid = document.getElementById("patient-preview-grid");
      const patientPreviewShare = document.getElementById("patient-preview-share");
      const patientPreviewCloseFooter = document.getElementById("patient-preview-close-footer");
      const mailCodeFeedback = document.getElementById("mail-code-feedback");
      const patientValidateButton = document.getElementById("patient-continue");
      const physicianDni = document.getElementById("physician-dni");
      const physicianPassword = document.getElementById("physician-password");
      const physicianDniError = document.getElementById("physician-dni-error");
      const physicianPasswordError = document.getElementById("physician-password-error");
      const physicianSearchPatientID = document.getElementById("physician-search-patient-id");
      const physicianSearchPatient = document.getElementById("physician-search-patient");
      const physicianFilterPeriod = document.getElementById("physician-filter-period");
      const physicianDateDropdown = document.getElementById("physician-date-dropdown");
      const physicianDateSummary = document.getElementById("physician-date-summary");
      const physicianCalendarMonthLabel = document.getElementById("physician-calendar-month-label");
      const physicianCalendarGrid = document.getElementById("physician-calendar-grid");
      const physicianCalendarPrev = document.getElementById("physician-calendar-prev");
      const physicianCalendarNext = document.getElementById("physician-calendar-next");
      const physicianSearchModality = document.getElementById("physician-search-modality");
      const physicianSearchSource = document.getElementById("physician-search-source");
      const physicianApplyFiltersButton = document.getElementById("physician-apply-filters");
      const physicianResultList = document.getElementById("physician-result-list");
      const operatorPanelToggle = document.getElementById("operator-panel-toggle");
      const operatorPanel = document.getElementById("operator-panel");
      const operatorWindowSelect = document.getElementById("operator-window-select");
      const operatorRefreshButton = document.getElementById("operator-refresh");
      const operatorSummaryCards = document.getElementById("operator-summary-cards");
      const operatorBreakdowns = document.getElementById("operator-breakdowns");
      const operatorEventsAction = document.getElementById("operator-events-action");
      const operatorEventsOutcome = document.getElementById("operator-events-outcome");
      const operatorEventsSearch = document.getElementById("operator-events-search");
      const operatorEventsTable = document.getElementById("operator-events-table");
      const operatorCommentsList = document.getElementById("operator-comments-list");
      const operatorCommentsRefresh = document.getElementById("operator-comments-refresh");
      const feedbackOpenButton = document.getElementById("feedback-open");
      const feedbackDialog = document.getElementById("feedback-dialog");
      const feedbackCloseButton = document.getElementById("feedback-close");
      const feedbackMessage = document.getElementById("feedback-message");
      const feedbackSendButton = document.getElementById("feedback-send");
      const feedbackStatus = document.getElementById("feedback-status");
      const physicianFullNameValue = document.getElementById("physician-full-name-value");
      const physicianDniValue = document.getElementById("physician-dni-value");
      const physicianLicenseValue = document.getElementById("physician-license-value");
      const physicianPacsHealthSummary = document.getElementById("physician-pacs-health-summary");
      const physicianPacsHealthText = document.getElementById("physician-pacs-health-text");
      const physicianPacsOnlineList = document.getElementById("physician-pacs-online-list");
      const physicianPacsOfflineList = document.getElementById("physician-pacs-offline-list");
      const physicianLoginButton = document.getElementById("physician-continue");
      const physicianNote = document.querySelector('[data-flow="physician"] .note');
      const resetButtons = document.querySelectorAll("[data-reset]");
      const screenAnchor = document.createComment("active-screen");
      shell.insertBefore(screenAnchor, hero);
      const flowAnchor = document.createComment("active-flow");
      authBody.insertBefore(flowAnchor, patientFlow);
      const patientShareQROverlayAnchor = document.createComment("patient-share-qr-overlay");
      patientShareQROverlay.parentNode.insertBefore(patientShareQROverlayAnchor, patientShareQROverlay);
      const patientPreviewOverlayAnchor = document.createComment("patient-preview-overlay");
      patientPreviewOverlay.parentNode.insertBefore(patientPreviewOverlayAnchor, patientPreviewOverlay);
      const mailCodeFeedbackAnchor = document.createComment("mail-code-feedback");
      mailCodeFeedback.parentNode.insertBefore(mailCodeFeedbackAnchor, mailCodeFeedback);
      const demoRibbonStates = Array.from(demoRibbons).map(ribbon => {
        const placeholder = document.createComment("demo-ribbon");
        ribbon.parentNode.insertBefore(placeholder, ribbon);
        return { ribbon, placeholder };
      });
      const physicianLocalCacheSourceValue = "local_cache";
      let activeRole = "patient";
      let activeScreen = "hero";
      let activeWorkspaceKind = "";
      let activePatientDocument = "";
      let activePhysicianUsername = "";
      let activePatientSearchRequestID = "";
      let activePatientSearchKey = "";
      let patientMailCodeReady = false;
      let patientShareQROpen = false;
      let patientPreviewOpen = false;
      let patientPreviewShareStudyUID = "";
      let patientSyncPollHandle = null;
      let patientRetrieveEventSource = null;
      let patientAutoRetrieveActiveStudyUID = "";
      let patientAutoRetrieveQueue = [];
      const patientStudyModalityByUID = new Map();
      let physicianRetrieveEventSource = null;
      let physicianAndesRefreshTimer = null;
      let physicianAndesRefreshRemaining = 0;
      let systemHealthEventSource = null;
      let portalSessionExpiresAt = "";
      let portalSessionTimeoutHandle = null;
      const portalWorkspaceStorageKey = "portalWorkspaceState";
      let portalSessionDurationMs = 10 * 60 * 1000;
      let portalShowDemoRibbon = false;
      let patientAuthMode = "mail";
      const patientDateFilter = (() => {
        const now = new Date();
        return {
          from: "",
          to: "",
          awaitingRangeEnd: false,
          viewYear: now.getFullYear(),
          viewMonth: now.getMonth()
        };
      })();
      const physicianDateFilter = (() => {
        const now = new Date();
        const defaultRange = patientDateRangeForPeriod("today");
        return {
          from: defaultRange.date_from || "",
          to: defaultRange.date_to || "",
          awaitingRangeEnd: false,
          viewYear: now.getFullYear(),
          viewMonth: now.getMonth()
        };
      })();

      function setActiveRoleFlow(role) {
        activeRole = role === "physician" ? "physician" : "patient";
        patientFlow.hidden = activeRole === "physician";
        physicianFlow.hidden = activeRole !== "physician";
        detachNode(patientFlow);
        detachNode(physicianFlow);
        mountNodeAfter(flowAnchor, activeRole === "physician" ? physicianFlow : patientFlow);
      }

      function activateRole(role) {
        roleButtons.forEach(button => {
          button.classList.toggle("active", button.dataset.role === role);
        });
        setActiveRoleFlow(role);
      }

      function showWorkspace(kind) {
        const workspace = kind === "patient" ? patientWorkspace : physicianWorkspace;
        patientWorkspace.hidden = kind !== "patient";
        physicianWorkspace.hidden = kind !== "physician";
        hero.hidden = true;
        workspace.hidden = false;
        detachNode(hero);
        detachNode(patientWorkspace);
        detachNode(physicianWorkspace);
        mountNodeAfter(screenAnchor, workspace);
        activeScreen = "workspace";
        activeWorkspaceKind = kind === "physician" ? "physician" : "patient";
        if (kind === "physician") {
          refreshPhysicianPACSHealth();
        }
        savePortalWorkspaceState(kind);
      }

      async function logoutPortalSession(kind) {
        const endpoints = kind === "patient"
          ? ["/api/patient/logout", "/api/physician/logout"]
          : kind === "physician"
            ? ["/api/physician/logout", "/api/patient/logout"]
            : ["/api/patient/logout", "/api/physician/logout"];
        for (const endpoint of endpoints) {
          try {
            await fetch(endpoint, {
              method: "POST",
              headers: {
                Accept: "application/json"
              }
            });
          } catch (_error) {
          }
        }
      }

      async function resetLanding() {
        const activeKind = activeWorkspaceKind;
        await logoutPortalSession(activeKind);
        clearPatientSyncPoll();
        clearPatientRetrievePoll();
        patientAutoRetrieveActiveStudyUID = "";
        patientAutoRetrieveQueue = [];
        clearPhysicianRetrievePoll();
        clearPhysicianAndesRefresh();
        setOperatorAccess(false);
        closePatientShareQR();
        closePatientPreview();
        clearPortalSession();
        detachNode(patientWorkspace);
        detachNode(physicianWorkspace);
        hero.hidden = false;
        mountNodeAfter(screenAnchor, hero);
        activeScreen = "hero";
        activeWorkspaceKind = "";
        patientSyncStatus.classList.remove("active", "error");
        patientSyncStatus.textContent = "";
        activePatientSearchRequestID = "";
        activePatientSearchKey = "";
        activePatientDocument = "";
        activePhysicianUsername = "";
        updateFeedbackAccess();
        clearLoginForms();
        clearPortalWorkspaceState();
        activateRole("patient");
      }

      function clearLoginForms() {
        patientDocument.value = "";
        patientMailCode.value = "";
        patientMailCodeReady = false;
        clearMailCodeFeedback();
        clearPatientLoginErrors();
        syncPatientContinueState();

        physicianDni.value = "";
        physicianPassword.value = "";
        clearPhysicianLoginErrors();
        physicianNote.classList.remove("warning");
        physicianNote.textContent = "Ingrese su DNI y contraseña institucional para acceder.";
        syncPhysicianContinueState();
      }

      function formatShareExpiration(value) {
        if (!value) {
          return "-";
        }

        const date = new Date(value);
        if (Number.isNaN(date.getTime())) {
          return value;
        }

        return new Intl.DateTimeFormat("es-AR", {
          dateStyle: "short",
          timeStyle: "short",
          timeZone: "UTC"
        }).format(date) + " UTC";
      }

      async function copyTextToClipboard(value) {
        if (!value) {
          return false;
        }

        if (navigator.clipboard?.writeText) {
          await navigator.clipboard.writeText(value);
          return true;
        }

        return false;
      }

      function openPatientShareQR(payload) {
        patientShareQRImage.src = payload.qr_code_data_url || "";
        patientShareQRLink.value = payload.share_url || "";
        patientShareQRExpiresAt.textContent = formatShareExpiration(payload.expires_at);
        patientShareQRMaxUses.textContent = String(payload.max_uses || "-");
        patientShareQRCopy.dataset.shareUrl = payload.share_url || "";
        patientShareQRWhatsApp.dataset.shareUrl = payload.whatsapp_url || "";
        patientShareQROverlay.hidden = false;
        mountNodeAfter(patientShareQROverlayAnchor, patientShareQROverlay);
        patientShareQROpen = true;
        patientShareQRClose.focus({ preventScroll: true });
      }

      function closePatientShareQR() {
        patientShareQROverlay.hidden = true;
        detachNode(patientShareQROverlay);
        patientShareQROpen = false;
      }

      function renderPatientPreview(payload) {
        const totalShown = Number(payload.total_shown || 0);
        const totalAvailable = Number(payload.total_available || totalShown);
        patientPreviewSummary.textContent = totalShown + "/" + totalAvailable + " imágenes del estudio.";

        patientPreviewGrid.innerHTML = (payload.items || []).map((item, index) => (
          '<article class="preview-item">' +
            '<img src="' + escapeHTML(item.image_data_url || "") + '" alt="Vista previa ' + String(index + 1) + '">' +
            '<a class="action ghost" href="' + escapeHTML(item.image_data_url || "") + '" download="' + escapeHTML(item.download_name || ("imagen-" + String(index + 1) + ".jpg")) + '">Descargar JPG</a>' +
          '</article>'
        )).join("");
      }

      function openPatientPreview(payload, options = {}) {
        renderPatientPreview(payload);
        patientPreviewShareStudyUID = options.shareEnabled ? String(payload.study_instance_uid || "") : "";
        patientPreviewShare.hidden = !patientPreviewShareStudyUID;
        patientPreviewOverlay.hidden = false;
        mountNodeAfter(patientPreviewOverlayAnchor, patientPreviewOverlay);
        patientPreviewOpen = true;
        patientPreviewCloseFooter.focus({ preventScroll: true });
      }

      function closePatientPreview() {
        patientPreviewOverlay.hidden = true;
        detachNode(patientPreviewOverlay);
        patientPreviewOpen = false;
        patientPreviewShareStudyUID = "";
      }

      function clearPortalSession() {
        portalSessionExpiresAt = "";
        if (portalSessionTimeoutHandle) {
          window.clearTimeout(portalSessionTimeoutHandle);
          portalSessionTimeoutHandle = null;
        }
      }

      function armPortalSessionTimeout() {
        if (!portalSessionExpiresAt) {
          return;
        }

        if (portalSessionTimeoutHandle) {
          window.clearTimeout(portalSessionTimeoutHandle);
          portalSessionTimeoutHandle = null;
        }

        const remainingMs = new Date(portalSessionExpiresAt).getTime() - Date.now();
        if (remainingMs <= 0) {
          returnToLandingSoft();
          return;
        }

        portalSessionTimeoutHandle = window.setTimeout(() => {
          returnToLandingSoft();
        }, remainingMs);
      }

      function startPortalSession() {
        portalSessionExpiresAt = new Date(Date.now() + portalSessionDurationMs).toISOString();
        armPortalSessionTimeout();
      }

      async function loadPortalRuntimeConfig() {
        try {
          const response = await fetch("/api/runtime-config", {
            headers: { Accept: "application/json" }
          });
          if (!response.ok) {
            return;
          }

          const payload = await response.json().catch(() => ({}));
          const minutes = Number(payload?.portal?.session_timeout_minutes);
          if (Number.isFinite(minutes) && minutes > 0) {
            portalSessionDurationMs = minutes * 60 * 1000;
          }
          portalShowDemoRibbon = Boolean(payload?.portal?.show_demo_ribbon);
          patientAuthMode = String(payload?.patient?.auth_mode || "mail").trim().toLowerCase() || "mail";
          applyDemoRibbonVisibility();
          applyPatientCodeInputMode();
        } catch (_error) {
        }
      }

      function applyDemoRibbonVisibility() {
        demoRibbonStates.forEach(({ ribbon, placeholder }) => {
          if (portalShowDemoRibbon) {
            ribbon.hidden = false;
            mountNodeAfter(placeholder, ribbon);
          } else {
            ribbon.hidden = true;
            detachNode(ribbon);
          }
        });
      }

      function applyPatientCodeInputMode() {
        const useMaskedInput = patientAuthMode === "master_key";
        patientMailCode.type = useMaskedInput ? "password" : "text";
        patientMailCode.inputMode = useMaskedInput ? "text" : "numeric";
        patientMailCode.autocomplete = useMaskedInput ? "off" : "one-time-code";
      }

      async function returnToLandingSoft() {
        await resetLanding();
        focusActiveRoleButton();
      }

      function focusActiveRoleButton() {
        window.requestAnimationFrame(() => {
          if (activeScreen === "hero") {
            const activeRoleButton = document.querySelector(".role-button.active");
            if (activeRoleButton instanceof HTMLElement) {
              activeRoleButton.focus({ preventScroll: true });
            }
          }
        });
      }

      function focusPatientDocumentInput() {
        window.requestAnimationFrame(() => {
          if (activeScreen === "hero" && activeRole === "patient") {
            patientDocument.focus({ preventScroll: true });
          }
        });
      }

      function focusPatientMailCodeInput() {
        window.requestAnimationFrame(() => {
          if (activeScreen === "hero" && activeRole === "patient") {
            patientMailCode.focus({ preventScroll: true });
            patientMailCode.select();
          }
        });
      }

      function focusPatientContinueButton() {
        window.requestAnimationFrame(() => {
          if (activeScreen === "hero" && activeRole === "patient") {
            patientValidateButton.focus({ preventScroll: true });
          }
        });
      }

      function focusPhysicianPasswordInput() {
        window.requestAnimationFrame(() => {
          if (activeScreen === "hero" && activeRole === "physician") {
            physicianPassword.focus({ preventScroll: true });
          }
        });
      }

      function focusPhysicianContinueButton() {
        window.requestAnimationFrame(() => {
          if (activeScreen === "hero" && activeRole === "physician") {
            physicianLoginButton.focus({ preventScroll: true });
          }
        });
      }

      function savePortalWorkspaceState(kindOverride = "") {
        const activeKind = kindOverride || activeWorkspaceKind;
        if (!activeKind) {
          sessionStorage.removeItem(portalWorkspaceStorageKey);
          return;
        }

        const state = { kind: activeKind, expires_at: portalSessionExpiresAt || "" };
        if (activeKind === "patient" && activePatientDocument) {
          state.patient = {
            document_number: activePatientDocument,
            date_from: patientDateFilter.from || "",
            date_to: patientDateFilter.to || "",
            modality: patientFilterModality.value || "",
            preset: patientFilterPeriod.value || "month"
          };
        }
        if (activeKind === "physician" && activePhysicianUsername) {
          state.physician = {
            username: activePhysicianUsername,
            patient_id: physicianSearchPatientID.value.trim(),
            patient_name: physicianSearchPatient.value.trim(),
            date_from: physicianDateFilter.from || "",
            date_to: physicianDateFilter.to || "",
            modality: physicianSearchModality.value.trim(),
            source: physicianSearchSource.value || physicianLocalCacheSourceValue
          };
        }

        sessionStorage.setItem(portalWorkspaceStorageKey, JSON.stringify(state));
      }

      function clearPortalWorkspaceState() {
        sessionStorage.removeItem(portalWorkspaceStorageKey);
      }

      async function restorePortalWorkspaceState() {
        const rawState = sessionStorage.getItem(portalWorkspaceStorageKey);
        if (!rawState) {
          clearLoginForms();
          return;
        }

        let state;
        try {
          state = JSON.parse(rawState);
        } catch (_error) {
          clearPortalWorkspaceState();
          clearLoginForms();
          return;
        }

        const expiresAt = typeof state?.expires_at === "string" ? state.expires_at : "";
        if (!expiresAt || Number.isNaN(new Date(expiresAt).getTime()) || new Date(expiresAt).getTime() <= Date.now()) {
          clearPortalWorkspaceState();
          clearLoginForms();
          focusActiveRoleButton();
          return;
        }

        portalSessionExpiresAt = expiresAt;
        armPortalSessionTimeout();

        if (state?.kind === "patient" && state.patient?.document_number) {
          activateRole("patient");
          patientDocument.value = state.patient.document_number;
          patientFilterModality.value = state.patient.modality || "";
          if (state.patient.date_from || state.patient.date_to) {
            patientFilterPeriod.value = state.patient.preset || "custom";
            patientDateFilter.from = state.patient.date_from || "";
            patientDateFilter.to = state.patient.date_to || "";
            patientDateFilter.awaitingRangeEnd = false;
            if (patientDateFilter.from) {
              const focusDate = new Date(patientDateFilter.from + "T00:00:00");
              patientDateFilter.viewYear = focusDate.getFullYear();
              patientDateFilter.viewMonth = focusDate.getMonth();
            }
            renderPatientCalendar();
          }
          showWorkspace("patient");
          try {
            await loadPatientStudies(state.patient.document_number);
          } catch (_error) {
            patientStudyList.innerHTML =
              '<div class="empty-state">No se pudieron restaurar los estudios del paciente.</div>';
          }
          return;
        }

        if (state?.kind === "physician" && state.physician?.username) {
          activateRole("physician");
          physicianDni.value = state.physician.username;
          physicianSearchPatientID.value = state.physician.patient_id || "";
          physicianSearchPatient.value = state.physician.patient_name || "";
          if (state.physician.date_from || state.physician.date_to) {
            physicianFilterPeriod.value = "custom";
            physicianDateFilter.from = state.physician.date_from || "";
            physicianDateFilter.to = state.physician.date_to || "";
            physicianDateFilter.awaitingRangeEnd = false;
            if (physicianDateFilter.from) {
              const focusDate = new Date(physicianDateFilter.from + "T00:00:00");
              physicianDateFilter.viewYear = focusDate.getFullYear();
              physicianDateFilter.viewMonth = focusDate.getMonth();
            }
            renderPhysicianCalendar();
          }
          physicianSearchModality.value = state.physician.modality || "";
          physicianSearchSource.value = state.physician.source || physicianLocalCacheSourceValue;
          showWorkspace("physician");
          try {
            await loadPhysicianResults(state.physician.username, {
              useInitialCachePeriod:
                !state.physician.patient_id &&
                !state.physician.patient_name &&
                !state.physician.date_from &&
                !state.physician.date_to &&
                !state.physician.modality &&
                (state.physician.source || physicianLocalCacheSourceValue) === physicianLocalCacheSourceValue
            });
          } catch (_error) {
            physicianResultList.innerHTML =
              '<div class="empty-state">No se pudieron restaurar los resultados del profesional.</div>';
          }
          return;
        }

        clearLoginForms();
      }

      function escapeHTML(value) {
        return String(value)
          .replaceAll("&", "&amp;")
          .replaceAll("<", "&lt;")
          .replaceAll(">", "&gt;")
          .replaceAll('"', "&quot;")
          .replaceAll("'", "&#39;");
      }

      // Wrapper used by restorePatientStudyFocus / restorePhysicianResultFocus /
      // updatePhysicianRetrieveVisual to look up a study card by its
      // StudyInstanceUID inside an attribute selector. CSS.escape is native in
      // every browser since 2014; keep a defensive fallback so a missing
      // CSS global cannot crash the silent retrieve refresh.
      function cssEscape(value) {
        const str = String(value == null ? "" : value);
        if (typeof CSS !== "undefined" && typeof CSS.escape === "function") {
          return CSS.escape(str);
        }
        return str.replace(/[^\w-]/g, (ch) => "\\" + ch);
      }

      function normalizePatientDocumentInput(value) {
        return String(value || "").replace(/\D+/g, "").slice(0, 11);
      }

      function normalizePhysicianDocumentInput(value) {
        return String(value || "").replace(/\D+/g, "").slice(0, 11);
      }

      function normalizePatientLookupIdentifierInput(value) {
        return String(value || "").replace(/\D+/g, "").slice(0, 11);
      }

      function clearFieldError(input, errorNode) {
        const fieldGroup = input?.closest(".field-group");
        if (fieldGroup) {
          fieldGroup.classList.remove("has-error");
        }
        if (input) {
          input.removeAttribute("aria-invalid");
        }
        if (errorNode) {
          errorNode.textContent = "";
        }
      }

      function setFieldError(input, errorNode, message) {
        const fieldGroup = input?.closest(".field-group");
        if (fieldGroup) {
          fieldGroup.classList.add("has-error");
        }
        if (input) {
          input.setAttribute("aria-invalid", "true");
        }
        if (errorNode) {
          errorNode.textContent = message || "";
        }
      }

      function clearPatientLoginErrors() {
        clearFieldError(patientDocument, patientDocumentError);
        clearFieldError(patientMailCode, patientMailCodeError);
      }

      function clearPhysicianLoginErrors() {
        clearFieldError(physicianDni, physicianDniError);
        clearFieldError(physicianPassword, physicianPasswordError);
      }

      function clearMailCodeFeedback() {
        mailCodeFeedback.hidden = true;
        mailCodeFeedback.classList.remove("warning");
        mailCodeFeedback.textContent = "";
        mailCodeFeedback.innerHTML = "";
        detachNode(mailCodeFeedback);
      }

      function setMailCodeFeedback(message, isWarning = false) {
        mailCodeFeedback.hidden = false;
        mountNodeAfter(mailCodeFeedbackAnchor, mailCodeFeedback);
        mailCodeFeedback.classList.remove("warning");
        mailCodeFeedback.textContent = "";
        mailCodeFeedback.innerHTML = message;
        if (isWarning) {
          mailCodeFeedback.classList.add("warning");
        }
      }

      function syncPatientContinueState() {
        const hasCode = patientMailCode.value.trim() !== "";
        patientValidateButton.disabled = !(patientMailCodeReady && hasCode);
      }

      function syncPhysicianContinueState() {
        const hasDocument = normalizePhysicianDocumentInput(physicianDni.value) !== "";
        const hasPassword = physicianPassword.value.trim() !== "";
        physicianLoginButton.disabled = !(hasDocument && hasPassword);
      }

      function clearPatientSyncPoll() {
        if (patientSyncPollHandle) {
          window.clearTimeout(patientSyncPollHandle);
          patientSyncPollHandle = null;
        }
      }

      function clearPatientRetrievePoll() {
        if (patientRetrieveEventSource) {
          patientRetrieveEventSource.close();
          patientRetrieveEventSource = null;
        }
      }

      function shouldAutoRetrievePatientStudy(study) {
        if (!study || !study.study_instance_uid) {
          return false;
        }
        if (study.viewer_url || study.download_url || study.retrieve_status === "done" || study.availability_status === "available_local") {
          return false;
        }
        if (study.retrieve_status === "running" || study.retrieve_status === "queued") {
          return false;
        }
        if (study.source_node_available === false || study.retrieve_status === "failed" || study.availability_status === "unavailable") {
          return false;
        }
        return true;
      }

      function enqueueAutoRetrieveForPatientStudies(studies) {
        const known = new Set(patientAutoRetrieveQueue);
        if (patientAutoRetrieveActiveStudyUID) {
          known.add(patientAutoRetrieveActiveStudyUID);
        }
        (studies || []).forEach(study => {
          if (!shouldAutoRetrievePatientStudy(study)) {
            return;
          }
          const studyUID = String(study.study_instance_uid || "");
          if (!studyUID || known.has(studyUID)) {
            return;
          }
          patientAutoRetrieveQueue.push(studyUID);
          known.add(studyUID);
          patientStudyModalityByUID.set(studyUID, (study.modalities || []).filter(Boolean).join("/"));
        });
      }

      async function processPatientAutoRetrieveQueue() {
        if (!activePatientDocument || patientAutoRetrieveActiveStudyUID || !patientAutoRetrieveQueue.length) {
          return;
        }

        const nextStudyUID = patientAutoRetrieveQueue.shift();
        if (!nextStudyUID) {
          return;
        }

        patientAutoRetrieveActiveStudyUID = nextStudyUID;
        try {
          const payload = await triggerPatientRetrieve(nextStudyUID, patientStudyModalityByUID.get(nextStudyUID) || "");
          if (payload?.job_id) {
            watchPatientRetrieveJob(payload.job_id, nextStudyUID);
            return;
          }
        } catch (_error) {
        }
        patientAutoRetrieveActiveStudyUID = "";
      }

      function clearPhysicianRetrievePoll() {
        if (physicianRetrieveEventSource) {
          physicianRetrieveEventSource.close();
          physicianRetrieveEventSource = null;
        }
      }

      function clearPhysicianAndesRefresh() {
        physicianAndesRefreshRemaining = 0;
        if (physicianAndesRefreshTimer) {
          window.clearTimeout(physicianAndesRefreshTimer);
          physicianAndesRefreshTimer = null;
        }
      }

      function schedulePhysicianAndesRefresh() {
        clearPhysicianAndesRefresh();
        if (!activePhysicianUsername) {
          return;
        }

        // Enrichment runs async in backend workers; perform short silent refreshes.
        physicianAndesRefreshRemaining = 2;
        const run = async () => {
          if (!activePhysicianUsername || activeWorkspaceKind !== "physician") {
            clearPhysicianAndesRefresh();
            return;
          }
          try {
            await loadPhysicianResults(activePhysicianUsername, {
              silentRefresh: true,
              fromAndesAutoRefresh: true
            });
          } catch (_error) {
          }
          physicianAndesRefreshRemaining -= 1;
          if (physicianAndesRefreshRemaining <= 0) {
            physicianAndesRefreshTimer = null;
            return;
          }
          physicianAndesRefreshTimer = window.setTimeout(run, 7000);
        };
        physicianAndesRefreshTimer = window.setTimeout(run, 7000);
      }

      function renderPatientSyncStatus(sync) {
        const status = sync?.status || "idle";
        if (sync?.request_id) {
          activePatientSearchRequestID = sync.request_id;
        }
        patientSyncStatus.classList.remove("active", "error");
        patientSyncStatus.textContent = "";

        if (status === "queued" || status === "running") {
          if (activePatientDocument) {
            activePatientSearchKey = patientSearchKeyForPayload(
              buildPatientSearchPayload(activePatientDocument)
            );
          }
          patientSyncStatus.classList.add("active");
          patientSyncStatus.textContent = sync.message || "Actualizando estudios...";
          clearPatientSyncPoll();
          patientSyncPollHandle = window.setTimeout(() => {
            if (activePatientDocument && activePatientSearchRequestID) {
              loadPatientSearchStatus(activePatientSearchRequestID).catch(() => {});
            }
          }, 1500);
          return;
        }

        clearPatientSyncPoll();
        activePatientSearchRequestID = "";
        activePatientSearchKey = "";
        if (status === "failed") {
          patientSyncStatus.classList.add("active", "error");
          patientSyncStatus.textContent = sync.message || "No se pudo actualizar la lista de estudios.";
        }
      }

      function buildPatientSearchPayload(documentNumber) {
        return {
          document_number: documentNumber,
          date_from: patientDateFilter.from || "",
          date_to: patientDateFilter.to || (patientDateFilter.from || ""),
          modality: patientFilterModality.value || ""
        };
      }

      function patientSearchKeyForPayload(payload) {
        return JSON.stringify(payload);
      }

      function chipClassForPatientCardState(study) {
        if (study?.viewer_url || study?.retrieve_status === "done" || study?.availability_status === "available_local") {
          return "chip success";
        }
        if (study?.source_node_available === false || study?.retrieve_status === "failed" || study?.availability_status === "unavailable") {
          return "chip";
        }
        return "chip warn";
      }

      function labelForPatientCardState(study) {
        if (study?.viewer_url || study?.retrieve_status === "done" || study?.availability_status === "available_local") {
          return "Disponible";
        }
        if (study?.source_node_available === false || study?.retrieve_status === "failed" || study?.availability_status === "unavailable") {
          return "No disponible";
        }
        return "En proceso";
      }

      function chipClassForPatientRetrieveState(status) {
        if (status === "done") return "chip success";
        if (status === "running" || status === "queued") return "chip warn";
        return "chip";
      }

      function labelForPatientRetrieveState(status) {
        if (status === "done") return "Disponible";
        if (status === "running" || status === "queued") return "En proceso";
        return "No disponible";
      }

      function modalityLabel(modality) {
        const code = (modality || "").trim().toUpperCase();
        const labels = {
          CR: "Radiografía computada",
          CT: "Tomografía computada",
          DOC: "Documento",
          DX: "Radiografía digital",
          KO: "Objetos clave",
          MG: "Mamografía",
          MR: "Resonancia magnética",
          NM: "Medicina nuclear",
          OT: "Otros estudios",
          PR: "Estado de presentacion",
          PT: "Tomografía por emisión de positrones",
          PX: "Radiografía panorámica",
          RF: "Radioscopia",
          SC: "Captura secundaria",
          SR: "Informe estructurado",
          US: "Ecografía",
          XA: "Angiografia por rayos X"
        };

        if (!code) {
          return "Sin modalidad";
        }
        if (!labels[code]) {
          return code;
        }
        if (code === "OT") {
          return labels[code];
        }
        return labels[code] + " (" + code + ")";
      }

      function formatModalityList(modalities) {
        return (modalities || []).map(modality => modalityLabel(modality)).join(", ");
      }

      function andesMetadataAvailable(item) {
        return !(item && item.his === false);
      }

      function andesMetadataLabel(item, key) {
        if (!andesMetadataAvailable(item)) {
          return "n/a";
        }
        return (item && item[key]) || "-";
      }

      function patientDateRangeForPeriod(period) {
        if (!period) {
          return {};
        }

        const now = new Date();
        const year = now.getFullYear();
        const month = now.getMonth();
        const day = now.getDate();

        const formatDate = value => value.toISOString().slice(0, 10);
        let fromDate = null;
        let toDate = new Date(year, month, day);

        if (period === "today") {
          fromDate = new Date(year, month, day);
        } else if (period === "week") {
          const currentDay = toDate.getDay();
          const diff = currentDay === 0 ? 6 : currentDay - 1;
          fromDate = new Date(year, month, day - diff);
        } else if (period === "month") {
          fromDate = new Date(year, month, 1);
        } else if (period === "year") {
          fromDate = new Date(year, 0, 1);
        }

        if (!fromDate) {
          return {};
        }

        return {
          date_from: formatDate(fromDate),
          date_to: formatDate(toDate)
        };
      }

      function formatDateISO(date) {
        return date.toISOString().slice(0, 10);
      }

      function formatDateLabel(value) {
        if (!value) {
          return "-";
        }

        return new Date(value + "T00:00:00").toLocaleDateString("es-AR");
      }

      function formatDICOMPersonName(value) {
        const raw = String(value || "").trim();
        if (!raw) {
          return "-";
        }

        if (!raw.includes("^")) {
          return raw.replace(/\s+/g, " ").trim();
        }

        const parts = raw
          .split("^")
          .map(part => part.replace(/\s+/g, " ").trim())
          .filter(Boolean);

        return parts.join(", ") || "-";
      }

      function syncPatientDateSummary() {
        if (patientDateFilter.from && patientDateFilter.to) {
          patientDateSummary.textContent =
            "Desde " + formatDateLabel(patientDateFilter.from) +
            " hasta " + formatDateLabel(patientDateFilter.to) + ".";
          return;
        }

        if (patientDateFilter.from) {
          if (patientDateFilter.awaitingRangeEnd) {
            patientDateSummary.textContent =
              "Inicio: " + formatDateLabel(patientDateFilter.from);
          } else {
            patientDateSummary.textContent =
              "Fecha seleccionada: " + formatDateLabel(patientDateFilter.from) + ".";
          }
          return;
        }

        patientDateSummary.textContent = "Sin rango seleccionado.";
      }

      function renderPatientCalendar() {
        const monthNames = [
          "Enero", "Febrero", "Marzo", "Abril", "Mayo", "Junio",
          "Julio", "Agosto", "Septiembre", "Octubre", "Noviembre", "Diciembre"
        ];
        patientCalendarMonthLabel.textContent =
          monthNames[patientDateFilter.viewMonth] + " " + patientDateFilter.viewYear;

        const firstDay = new Date(patientDateFilter.viewYear, patientDateFilter.viewMonth, 1);
        const firstWeekday = (firstDay.getDay() + 6) % 7;
        const gridStart = new Date(patientDateFilter.viewYear, patientDateFilter.viewMonth, 1 - firstWeekday);
        const from = patientDateFilter.from ? new Date(patientDateFilter.from + "T00:00:00") : null;
        const to = patientDateFilter.to ? new Date(patientDateFilter.to + "T00:00:00") : null;

        patientCalendarGrid.innerHTML = Array.from({ length: 42 }, (_, index) => {
          const date = new Date(gridStart.getFullYear(), gridStart.getMonth(), gridStart.getDate() + index);
          const dateISO = formatDateISO(date);
          const isCurrentMonth = date.getMonth() === patientDateFilter.viewMonth;
          const isSelected = patientDateFilter.from === dateISO || patientDateFilter.to === dateISO;
          const isInRange = from && to && date >= from && date <= to;
          const classes = [
            "calendar-day",
            !isCurrentMonth ? "is-muted" : "",
            isInRange ? "is-in-range" : "",
            isSelected ? "is-selected" : ""
          ].filter(Boolean).join(" ");

          return (
            '<button class="' + classes + '" type="button" data-patient-calendar-day="' + dateISO + '">' +
              date.getDate() +
            '</button>'
          );
        }).join("");

        syncPatientDateSummary();
      }

      function applyPatientPreset(period) {
        patientFilterPeriod.value = period;
        const range = patientDateRangeForPeriod(period);
        patientDateFilter.from = range.date_from || "";
        patientDateFilter.to = range.date_to || "";
        patientDateFilter.awaitingRangeEnd = false;

        if (patientDateFilter.from) {
          const focusDate = new Date(patientDateFilter.from + "T00:00:00");
          patientDateFilter.viewYear = focusDate.getFullYear();
          patientDateFilter.viewMonth = focusDate.getMonth();
        } else {
          const now = new Date();
          patientDateFilter.viewYear = now.getFullYear();
          patientDateFilter.viewMonth = now.getMonth();
        }

        renderPatientCalendar();
        patientDateDropdown.open = period === "custom";
      }

      function selectPatientCalendarDate(dateISO) {
        if (!patientDateFilter.from || (patientDateFilter.from && patientDateFilter.to)) {
          patientDateFilter.from = dateISO;
          patientDateFilter.to = "";
          patientDateFilter.awaitingRangeEnd = true;
        } else if (dateISO === patientDateFilter.from) {
          patientDateFilter.to = "";
          patientDateFilter.awaitingRangeEnd = false;
        } else if (dateISO < patientDateFilter.from) {
          patientDateFilter.to = patientDateFilter.from;
          patientDateFilter.from = dateISO;
          patientDateFilter.awaitingRangeEnd = false;
        } else {
          patientDateFilter.to = dateISO;
          patientDateFilter.awaitingRangeEnd = false;
        }

        patientFilterPeriod.value = "custom";
        renderPatientCalendar();
      }

      function syncPhysicianDateSummary() {
        if (physicianDateFilter.from && physicianDateFilter.to) {
          physicianDateSummary.textContent =
            "Desde " + formatDateLabel(physicianDateFilter.from) +
            " hasta " + formatDateLabel(physicianDateFilter.to) + ".";
          return;
        }

        if (physicianDateFilter.from) {
          if (physicianDateFilter.awaitingRangeEnd) {
            physicianDateSummary.textContent =
              "Inicio: " + formatDateLabel(physicianDateFilter.from);
          } else {
            physicianDateSummary.textContent =
              "Fecha seleccionada: " + formatDateLabel(physicianDateFilter.from) + ".";
          }
          return;
        }

        physicianDateSummary.textContent = "Sin rango seleccionado.";
      }

      function renderPhysicianCalendar() {
        const monthNames = [
          "Enero", "Febrero", "Marzo", "Abril", "Mayo", "Junio",
          "Julio", "Agosto", "Septiembre", "Octubre", "Noviembre", "Diciembre"
        ];
        physicianCalendarMonthLabel.textContent =
          monthNames[physicianDateFilter.viewMonth] + " " + physicianDateFilter.viewYear;

        const firstDay = new Date(physicianDateFilter.viewYear, physicianDateFilter.viewMonth, 1);
        const firstWeekday = (firstDay.getDay() + 6) % 7;
        const gridStart = new Date(physicianDateFilter.viewYear, physicianDateFilter.viewMonth, 1 - firstWeekday);
        const from = physicianDateFilter.from ? new Date(physicianDateFilter.from + "T00:00:00") : null;
        const to = physicianDateFilter.to ? new Date(physicianDateFilter.to + "T00:00:00") : null;

        physicianCalendarGrid.innerHTML = Array.from({ length: 42 }, (_, index) => {
          const date = new Date(gridStart.getFullYear(), gridStart.getMonth(), gridStart.getDate() + index);
          const dateISO = formatDateISO(date);
          const isCurrentMonth = date.getMonth() === physicianDateFilter.viewMonth;
          const isSelected = physicianDateFilter.from === dateISO || physicianDateFilter.to === dateISO;
          const isInRange = from && to && date >= from && date <= to;
          const classes = [
            "calendar-day",
            !isCurrentMonth ? "is-muted" : "",
            isInRange ? "is-in-range" : "",
            isSelected ? "is-selected" : ""
          ].filter(Boolean).join(" ");

          return (
            '<button class="' + classes + '" type="button" data-physician-calendar-day="' + dateISO + '">' +
              date.getDate() +
            '</button>'
          );
        }).join("");

        syncPhysicianDateSummary();
      }

      function applyPhysicianPreset(period) {
        physicianFilterPeriod.value = period;
        const range = patientDateRangeForPeriod(period);
        physicianDateFilter.from = range.date_from || "";
        physicianDateFilter.to = range.date_to || "";
        physicianDateFilter.awaitingRangeEnd = false;

        if (physicianDateFilter.from) {
          const focusDate = new Date(physicianDateFilter.from + "T00:00:00");
          physicianDateFilter.viewYear = focusDate.getFullYear();
          physicianDateFilter.viewMonth = focusDate.getMonth();
        } else {
          const now = new Date();
          physicianDateFilter.viewYear = now.getFullYear();
          physicianDateFilter.viewMonth = now.getMonth();
        }

        renderPhysicianCalendar();
        physicianDateDropdown.open = period === "custom";
      }

      function selectPhysicianCalendarDate(dateISO) {
        if (!physicianDateFilter.from || (physicianDateFilter.from && physicianDateFilter.to)) {
          physicianDateFilter.from = dateISO;
          physicianDateFilter.to = "";
          physicianDateFilter.awaitingRangeEnd = true;
        } else if (dateISO === physicianDateFilter.from) {
          physicianDateFilter.to = "";
          physicianDateFilter.awaitingRangeEnd = false;
        } else if (dateISO < physicianDateFilter.from) {
          physicianDateFilter.to = physicianDateFilter.from;
          physicianDateFilter.from = dateISO;
          physicianDateFilter.awaitingRangeEnd = false;
        } else {
          physicianDateFilter.to = dateISO;
          physicianDateFilter.awaitingRangeEnd = false;
        }

        physicianFilterPeriod.value = "custom";
        renderPhysicianCalendar();
      }

      function previewCalendarRange(filter, grid, summaryEl, attr, hoverISO) {
        if (!filter.awaitingRangeEnd || !filter.from || !hoverISO) {
          return;
        }
        const start = filter.from < hoverISO ? filter.from : hoverISO;
        const end = filter.from < hoverISO ? hoverISO : filter.from;
        grid.querySelectorAll("[" + attr + "]").forEach(button => {
          const iso = button.getAttribute(attr);
          const inPreview = iso >= start && iso <= end && !button.classList.contains("is-selected");
          button.classList.toggle("is-preview", inPreview);
        });
        summaryEl.textContent =
          "Desde " + formatDateLabel(start) + " hasta " + formatDateLabel(end) + ".";
      }

      function clearCalendarPreview(grid, attr, syncSummary) {
        grid.querySelectorAll("[" + attr + "].is-preview").forEach(button => {
          button.classList.remove("is-preview");
        });
        syncSummary();
      }

      function setOperatorAccess(canView) {
        if (!operatorPanelToggle) {
          return;
        }
        if (canView) {
          operatorPanelToggle.hidden = false;
          return;
        }
        operatorPanelToggle.hidden = true;
        operatorPanelToggle.setAttribute("aria-pressed", "false");
        operatorPanelToggle.textContent = "Métricas y auditoría";
        if (operatorPanel) {
          operatorPanel.hidden = true;
          if (operatorPanel.parentElement) {
            operatorPanel.parentElement.classList.remove("operator-open");
          }
        }
      }

      function updateFeedbackAccess() {
        if (!feedbackOpenButton) {
          return;
        }
        const loggedIn = Boolean(activePatientDocument || activePhysicianUsername);
        feedbackOpenButton.hidden = !loggedIn;
        if (!loggedIn && feedbackDialog && !feedbackDialog.hidden) {
          feedbackDialog.hidden = true;
        }
      }

      function operatorWindow() {
        return operatorWindowSelect ? operatorWindowSelect.value : "7d";
      }

      async function fetchOperatorJSON(path) {
        const response = await fetch(path, { headers: { Accept: "application/json" } });
        if (!response.ok) {
          const err = new Error("operator_request_failed");
          err.status = response.status;
          throw err;
        }
        return response.json();
      }

      function operatorOutcomeBadge(outcome) {
        const safe = String(outcome || "").toLowerCase();
        const known = safe === "success" || safe === "denied" || safe === "failure";
        const cls = known ? " outcome-" + safe : "";
        return '<span class="outcome-badge' + cls + '">' + escapeHTML(outcome || "-") + "</span>";
      }

      function operatorTimestamp(iso) {
        if (!iso) {
          return "-";
        }
        const date = new Date(iso);
        if (Number.isNaN(date.getTime())) {
          return escapeHTML(iso);
        }
        return date.toLocaleString("es-AR", { dateStyle: "short", timeStyle: "medium" });
      }

      async function loadOperatorUsage() {
        await Promise.all([loadOperatorSummary(), loadOperatorEvents(), loadOperatorComments()]);
      }

      function operatorCommentAuthor(comment) {
        const name = String(comment.actor_name || "").trim();
        const kindLabels = { patient: "Paciente", physician: "Profesional", public: "Anónimo" };
        const kind = kindLabels[comment.actor_kind] || comment.actor_kind || "Anónimo";
        const role = String(comment.actor_role || "").trim();
        let label = name || kind;
        const suffix = name ? kind : "";
        const parts = [label];
        if (suffix && suffix !== label) {
          parts.push(suffix);
        }
        if (role && role !== "public") {
          parts.push(role);
        }
        return parts.join(" · ");
      }

      async function loadOperatorComments() {
        if (!operatorCommentsList) {
          return;
        }
        operatorCommentsList.innerHTML = '<div class="empty-state">Cargando comentarios...</div>';
        try {
          const payload = await fetchOperatorJSON("/api/operator/feedback?limit=50");
          const comments = payload.comments || [];
          if (!comments.length) {
            operatorCommentsList.innerHTML = '<div class="empty-state">Sin comentarios.</div>';
            return;
          }
          operatorCommentsList.innerHTML = comments
            .map(comment =>
              '<article class="operator-comment">' +
                '<div class="operator-comment-meta">' +
                  '<span class="operator-comment-author">' + escapeHTML(operatorCommentAuthor(comment)) + "</span>" +
                  '<span class="operator-comment-time">' + escapeHTML(operatorTimestamp(comment.created_at)) + "</span>" +
                "</div>" +
                '<div class="operator-comment-body">' + escapeHTML(comment.message || "") + "</div>" +
              "</article>"
            )
            .join("");
        } catch (error) {
          const msg = error?.status === 403
            ? "No tiene permisos para ver comentarios."
            : "No se pudieron cargar los comentarios.";
          operatorCommentsList.innerHTML = '<div class="empty-state">' + msg + "</div>";
        }
      }

      async function loadOperatorSummary() {
        operatorSummaryCards.innerHTML = '<div class="empty-state">Cargando métricas...</div>';
        try {
          const payload = await fetchOperatorJSON(
            "/api/operator/usage/summary?window=" + encodeURIComponent(operatorWindow())
          );
          renderOperatorSummary(payload);
        } catch (error) {
          const msg = error?.status === 403
            ? "No tiene permisos para ver métricas."
            : "No se pudieron cargar las métricas.";
          operatorSummaryCards.innerHTML = '<div class="empty-state">' + msg + "</div>";
          operatorBreakdowns.innerHTML = "";
        }
      }

      function renderOperatorSummary(payload) {
        const byOutcome = {};
        (payload.by_outcome || []).forEach(item => {
          byOutcome[item.outcome] = item.total;
        });
        const logins = payload.logins || {};
        const cards = [
          { label: "Acciones totales", value: payload.total || 0 },
          { label: "Éxito", value: byOutcome.success || 0 },
          { label: "Denegadas", value: byOutcome.denied || 0 },
          { label: "Fallidas", value: byOutcome.failure || 0 },
          { label: "Pacientes únicos", value: logins.unique_patients || 0 },
          { label: "Logins de pacientes", value: logins.patient_logins || 0 },
          { label: "Profesionales únicos", value: logins.unique_physician_logins || 0 },
          { label: "Logins de profesionales", value: logins.physician_logins || 0 }
        ];
        operatorSummaryCards.innerHTML = cards
          .map(card =>
            '<div class="operator-metric-card"><div class="metric-label">' +
            escapeHTML(card.label) +
            '</div><div class="metric-value">' +
            escapeHTML(String(card.value)) +
            "</div></div>"
          )
          .join("");

        const actions = payload.by_action || [];
        const previous = operatorEventsAction.value;
        operatorEventsAction.innerHTML =
          '<option value="">Todas</option>' +
          actions
            .map(item => '<option value="' + escapeHTML(item.action) + '">' + escapeHTML(item.action) + "</option>")
            .join("");
        operatorEventsAction.value = previous;

        if (!actions.length) {
          operatorBreakdowns.innerHTML = '<div class="empty-state">Sin acciones en la ventana seleccionada.</div>';
          return;
        }
        const rows = actions
          .map(item =>
            "<tr><td>" + escapeHTML(item.action) + "</td><td>" + (item.total || 0) +
            "</td><td>" + (item.success || 0) + "</td><td>" + (item.denied || 0) +
            "</td><td>" + (item.failure || 0) + "</td><td>" + (item.avg_latency_ms || 0) +
            " ms</td><td>" + (item.p95_latency_ms || 0) + " ms</td></tr>"
          )
          .join("");
        const actionTable =
          "<table><thead><tr><th>Acción</th><th>Total</th><th>OK</th><th>Deneg.</th><th>Fallo</th><th>Lat. prom</th><th>p95</th></tr></thead><tbody>" +
          rows +
          "</tbody></table>";

        const statuses = payload.by_status || [];
        let statusTable = "";
        if (statuses.length) {
          const statusRows = statuses
            .map(item => {
              const code = item.status_code || 0;
              const label = code === 0 ? "Sin código" : String(code);
              return "<tr><td>" + escapeHTML(label) + "</td><td>" + (item.total || 0) + "</td></tr>";
            })
            .join("");
          statusTable =
            "<table><thead><tr><th>Código HTTP</th><th>Cantidad</th></tr></thead><tbody>" +
            statusRows +
            "</tbody></table>";
        }

        const modalities = payload.downloads_by_modality || [];
        let modalityTable = "";
        if (modalities.length) {
          const modalityRows = modalities
            .map(item =>
              "<tr><td>" + escapeHTML(item.modality || "UNKNOWN") + "</td><td>" + (item.total || 0) + "</td></tr>"
            )
            .join("");
          modalityTable =
            "<table><thead><tr><th>Descargas por modalidad</th><th>Cantidad</th></tr></thead><tbody>" +
            modalityRows +
            "</tbody></table>";
        }

        const retrieveModalities = payload.retrieves_by_modality || [];
        let retrieveModalityTable = "";
        if (retrieveModalities.length) {
          const retrieveModalityRows = retrieveModalities
            .map(item =>
              "<tr><td>" + escapeHTML(item.modality || "UNKNOWN") + "</td><td>" + (item.total || 0) + "</td></tr>"
            )
            .join("");
          retrieveModalityTable =
            "<table><thead><tr><th>Retrieves por modalidad</th><th>Cantidad</th></tr></thead><tbody>" +
            retrieveModalityRows +
            "</tbody></table>";
        }

        const retrievers = payload.top_physician_retrievers || [];
        let retrieverTable = "";
        if (retrievers.length) {
          const retrieverRows = retrievers
            .map(item =>
              "<tr><td>" + escapeHTML(item.dni || "—") + "</td><td>" + (item.total || 0) + "</td></tr>"
            )
            .join("");
          retrieverTable =
            "<table><thead><tr><th>Top profesionales por retrieves (DNI)</th><th>Cantidad</th></tr></thead><tbody>" +
            retrieverRows +
            "</tbody></table>";
        }

        operatorBreakdowns.innerHTML =
          '<div class="operator-breakdown-full">' + actionTable + "</div>" +
          '<div class="operator-breakdown-grid">' + statusTable + modalityTable + retrieveModalityTable + retrieverTable + "</div>";
      }

      async function loadOperatorEvents() {
        operatorEventsTable.innerHTML = '<div class="empty-state">Cargando acciones...</div>';
        const params = new URLSearchParams({ window: operatorWindow(), limit: "100" });
        if (operatorEventsAction.value) {
          params.set("action", operatorEventsAction.value);
        }
        if (operatorEventsOutcome.value) {
          params.set("outcome", operatorEventsOutcome.value);
        }
        if (operatorEventsSearch && operatorEventsSearch.value.trim()) {
          params.set("q", operatorEventsSearch.value.trim());
        }
        try {
          const payload = await fetchOperatorJSON("/api/operator/usage/events?" + params.toString());
          renderOperatorEvents(payload);
        } catch (error) {
          const msg = error?.status === 403
            ? "No tiene permisos para ver la auditoría."
            : "No se pudieron cargar las acciones.";
          operatorEventsTable.innerHTML = '<div class="empty-state">' + msg + "</div>";
        }
      }

      function renderOperatorEvents(payload) {
        const events = payload.events || [];
        if (!events.length) {
          operatorEventsTable.innerHTML = '<div class="empty-state">Sin acciones registradas.</div>';
          return;
        }
        const rows = events
          .map(evt => {
            let dims = "";
            try {
              dims = JSON.stringify(evt.dims || {});
            } catch (_) {
              dims = "";
            }
            if (dims === "{}") {
              dims = "";
            }
            const actor = [evt.actor_role, evt.actor_id].filter(Boolean).join(" · ") || evt.actor_kind || "-";
            return "<tr><td>" + operatorTimestamp(evt.occurred_at) + "</td><td>" +
              escapeHTML(evt.action) + "</td><td>" + escapeHTML(actor) + "</td><td>" +
              operatorOutcomeBadge(evt.outcome) + "</td><td>" + (evt.status_code || "-") +
              "</td><td>" + (evt.latency_ms || 0) + " ms</td><td>" + escapeHTML(dims) + "</td></tr>";
          })
          .join("");
        operatorEventsTable.innerHTML =
          "<table><thead><tr><th>Fecha</th><th>Acción</th><th>Actor</th><th>Resultado</th><th>HTTP</th><th>Latencia</th><th>Dims</th></tr></thead><tbody>" +
          rows +
          "</tbody></table>";
      }

      function renderPatientStudies(payload) {
        renderPatientSyncStatus(payload.sync);
        patientFullNameValue.textContent = payload.patient.full_name || "-";
        patientDocumentValue.textContent = payload.patient.document_number;
        patientBirthDateValue.textContent = payload.patient.birth_date || "-";
        patientSexValue.textContent = payload.patient.sex || "-";
        patientGenderIdentityValue.textContent = payload.patient.gender_identity || "-";

        if (!payload.studies.length) {
          const hasFilters =
            Boolean(payload.filters?.date_from) ||
            Boolean(payload.filters?.date_to) ||
            Boolean(payload.filters?.modality);
          patientStudyList.innerHTML =
            hasFilters
              ? '<div class="empty-state">No hay estudios para los filtros seleccionados.</div>'
              : '<div class="empty-state">No se encontraron estudios para este documento.</div>';
          return;
        }

        patientStudyList.innerHTML =
          payload.studies
            .map(study => {
              const modalities = (study.modalities_in_study || [])
                .map(modality => '<div class="chip info">' + escapeHTML(modalityLabel(modality)) + "</div>")
                .join("");

              const action = study.viewer_url
                ? '<button class="action action-primary" type="button" data-patient-viewer="' + escapeHTML(study.study_instance_uid) + '" data-viewer-kind="stone">Visualizar</button>'
                : "";
              const previewAction = study.viewer_url
                ? '<button class="action action-viewer-preferred" type="button" data-patient-preview="' + escapeHTML(study.study_instance_uid) + '">Vista previa</button>'
                : "";
              const downloadAction = study.download_url
                ? '<a class="action action-primary" href="' + escapeHTML(study.download_url) + '" rel="noopener noreferrer">Descargar DICOM</a>'
                : "";
              const andesReportAction = study.andes_prestacion_id
                ? '<a class="action action-primary" href="/api/patient/report?study_instance_uid=' + encodeURIComponent(study.study_instance_uid || "") + '" rel="noopener noreferrer">Descargar informe</a>'
                : "";
              const shareAction = study.viewer_url
                ? '<button class="action action-primary" type="button" data-patient-share="' + escapeHTML(study.study_instance_uid) + '" data-viewer-kind="stone">Compartir</button>'
                : "";
              const imageCount = Number(study.number_of_images || 0);
              const imageCountLabel = imageCount > 0 ? String(imageCount) : "-";

              const detailChips =
                '<div class="chip-row">' +
                  '<div class="' + chipClassForPatientCardState(study) + '" data-patient-status-chip="' + escapeHTML(study.study_instance_uid) + '">' +
                    escapeHTML(labelForPatientCardState(study)) +
                  '</div>' +
                  (study.locations || []).map(location => '<div class="chip success">Hospital: ' + escapeHTML(location) + '</div>').join("") +
                  modalities +
                '</div>';

              return (
                '<article class="study-item" data-patient-study="' + escapeHTML(study.study_instance_uid) + '">' +
                  '<div class="item-head">' +
                    '<div>' +
                      '<strong>Descripción del estudio: ' + escapeHTML(study.study_description || "Estudio sin descripción") + '</strong>' +
                      '<span>' + escapeHTML(study.study_date || "Sin fecha") + '</span>' +
                    '</div>' +
                  '</div>' +
                  '<div class="meta-grid">' +
                  '<div><span>Prestación en ANDES</span><strong>' + escapeHTML(andesMetadataLabel(study, "andes_prestacion")) + '</strong></div>' +
                  '<div><span>Profesional en ANDES</span><strong>' + escapeHTML(andesMetadataLabel(study, "andes_professional")) + '</strong></div>' +
                  '<div><span>Imágenes</span><strong>' + escapeHTML(imageCountLabel) + '</strong></div>' +
                '</div>' +
                  detailChips +
                  '<div class="inline-actions">' + previewAction + action + shareAction + andesReportAction + downloadAction + '</div>' +
                '</article>'
              );
            })
            .join("");
      }

      function retrievePhaseLabel(phase) {
        if (phase === "preparing") return "Preparando recuperación";
        if (phase === "retrieving") return "Recuperando desde origen";
        if (phase === "verifying") return "Verificando disponibilidad local";
        if (phase === "paused") return "Recuperación pausada";
        if (phase === "done") return "Recuperación completa";
        return "";
      }

      function labelForRetrieveStatus(status, phase, progress) {
        if (status === "idle") return "Recuperación pendiente";
        if (status === "done") return "Recuperación completa";
        if (status === "running") {
          const phaseLabel = retrievePhaseLabel(phase);
          if (typeof progress === "number" && progress > 0 && progress < 100) {
            return (phaseLabel || "Recuperación en curso") + " (" + progress + "%)";
          }
          return phaseLabel || "Recuperación en curso";
        }
        if (status === "queued") return retrievePhaseLabel(phase) || "Recuperación en cola";
        if (status === "failed") return "Recuperación con error";
        return status || "Estado desconocido";
      }

      function chipClassForRetrieve(status) {
        if (status === "done") return "chip success";
        if (status === "running" || status === "queued") return "chip warn";
        if (status === "failed") return "chip";
        return "chip";
      }

      function retrieveActionLabel(status, phase, progress) {
        if (status === "running" || status === "queued") {
          if (typeof progress === "number" && progress > 0 && progress < 100) {
            return "Recuperando " + progress + "%";
          }
          return retrievePhaseLabel(phase) || "Recuperando";
        }
        if (status === "done") return "Recuperado";
        if (status === "failed") return "Reintentar recuperación";
        return "Recuperar estudio";
      }

      function labelForCacheStatus(status) {
        if (status === "local_complete") return "Completo";
        if (status === "local_partial") return "Parcial";
        if (status === "not_local") return "No disponible localmente";
        return status || "Desconocido";
      }

      function formatPACSList(items) {
        if (!items.length) {
          return "Ninguno.";
        }
        return items.join("\n");
      }

      function upsertPhysicianSourceOptions(onlineComponents) {
        const selectedValue = physicianSearchSource.value || physicianLocalCacheSourceValue;
        const options = [
          { value: physicianLocalCacheSourceValue, label: "Local" }
        ];

        (onlineComponents || []).forEach(component => {
          const nodeID = String(component.name || "").replace(/^remote_pacs:/, "").trim();
          if (!nodeID) {
            return;
          }
          options.push({
            value: nodeID,
            label: component.display_name || nodeID
          });
        });

        physicianSearchSource.innerHTML = options
          .map(option => '<option value="' + escapeHTML(option.value) + '">' + escapeHTML(option.label) + '</option>')
          .join("");

        const availableValues = new Set(options.map(option => option.value));
        physicianSearchSource.value = availableValues.has(selectedValue) ? selectedValue : physicianLocalCacheSourceValue;
      }

      function hasPhysicianQueryFilters(filters) {
        return Boolean(filters?.patient_id) ||
          Boolean(filters?.patient_name) ||
          Boolean(filters?.date_from) ||
          Boolean(filters?.date_to) ||
          Boolean(filters?.modality);
      }

      function renderPhysicianPACSHealth(components) {
        const remoteComponents = (components || []).filter(component => component.name && component.name.startsWith("remote_pacs:"));
        const total = remoteComponents.length;
        const online = remoteComponents
          .filter(component => component.status === "healthy")
          .map(component => component.display_name || component.name.replace("remote_pacs:", ""));
        const offline = remoteComponents
          .filter(component => component.status !== "healthy")
          .map(component => component.display_name || component.name.replace("remote_pacs:", ""));
        const onlineComponents = remoteComponents.filter(component => component.status === "healthy");

        physicianPacsHealthText.textContent = "PACS en línea " + online.length + "/" + total;
        physicianPacsOnlineList.textContent = formatPACSList(online);
        physicianPacsOfflineList.textContent = formatPACSList(offline);
        upsertPhysicianSourceOptions(onlineComponents);

        let status = "ok";
        if (total === 0) {
          status = "unknown";
        } else if (online.length === 0) {
          status = "error";
        } else if (online.length < total) {
          status = "warn";
        }
        physicianPacsHealthSummary.dataset.status = status;
      }

      function closePhysicianPACSHealthSummary() {
        physicianPacsHealthSummary.classList.remove("is-open");
        physicianPacsHealthSummary.setAttribute("aria-expanded", "false");
      }

      function openPhysicianPACSHealthSummary() {
        physicianPacsHealthSummary.classList.add("is-open");
        physicianPacsHealthSummary.setAttribute("aria-expanded", "true");
      }

      function togglePhysicianPACSHealthSummary() {
        if (physicianPacsHealthSummary.classList.contains("is-open")) {
          closePhysicianPACSHealthSummary();
          return;
        }
        openPhysicianPACSHealthSummary();
      }

      async function fetchDetailedHealth() {
        const response = await fetch("/api/health", {
          cache: "no-store",
          headers: {
            Accept: "application/json",
            "X-Portal-Internal-Health": "1"
          }
        });

        const payload = await response.json().catch(() => ({}));
        if (!response.ok && response.status !== 503) {
          throw new Error("health request failed");
        }
        return payload;
      }

      async function refreshPhysicianPACSHealth() {
        try {
          const payload = await fetchDetailedHealth();
          renderPhysicianPACSHealth(payload.components || []);
        } catch (error) {
          renderPhysicianPACSHealth([]);
        }
      }

      function renderPhysicianResults(payload) {
        physicianFullNameValue.textContent = payload.physician.full_name || "-";
        physicianDniValue.textContent = payload.physician.dni || "-";
        physicianLicenseValue.textContent = payload.physician.license_number || "-";
        const canShare = Boolean(payload.can_share);
        if (typeof payload.can_view_metrics !== "undefined") {
          setOperatorAccess(Boolean(payload.can_view_metrics));
        }

        if (!payload.results.length) {
          physicianResultList.innerHTML =
            hasPhysicianQueryFilters(payload.filters)
              ? '<div class="empty-state">No hay resultados para los filtros seleccionados.</div>'
              : '<div class="empty-state">No hay estudios locales para el rango temporal configurado.</div>';
          return;
        }

        physicianResultList.innerHTML = payload.results
          .map(result => {
            const modalities = (result.modalities || [])
              .map(modality => '<div class="chip info">' + escapeHTML(modalityLabel(modality)) + "</div>")
              .join("");

            const action = result.viewer_url
              ? '<button class="action action-viewer-preferred" type="button" data-physician-viewer="' + escapeHTML(result.study_instance_uid) + '" data-viewer-kind="stone">Visualizar</button>'
              : "";
            const ohifAction = result.ohif_viewer_url
              ? '<button class="action action-primary" type="button" data-physician-viewer="' + escapeHTML(result.study_instance_uid) + '" data-viewer-kind="ohif">Visualizador Alternativo</button>'
              : "";
            const previewAction = result.viewer_url
              ? '<button class="action action-primary" type="button" data-physician-preview="' + escapeHTML(result.study_instance_uid) + '">Vista previa</button>'
              : "";
            const shareAction = canShare && result.viewer_url
              ? '<button class="action action-primary" type="button" data-physician-share="' + escapeHTML(result.study_instance_uid) + '" data-viewer-kind="stone">Compartir</button>'
              : "";
            const downloadAction = result.download_url
              ? '<a class="action action-primary" href="' + escapeHTML(result.download_url) + '" rel="noopener noreferrer">Descargar DICOM</a>'
              : "";
            const andesReportAction = result.andes_prestacion_id
              ? '<a class="action action-primary" href="/api/physician/report?study_instance_uid=' + encodeURIComponent(result.study_instance_uid || "") + '" rel="noopener noreferrer">Descargar informe</a>'
              : "";
            const imageCount = Number(result.number_of_images || 0);
            const imageCountLabel = imageCount > 0 ? String(imageCount) : "-";
            const patientNameLabel = formatDICOMPersonName(result.patient_name);
            const studyDateLabel = result.study_date ? formatDateLabel(result.study_date) : "Sin fecha";
            const retrieveSourceNode = result.source_node_id ||
              (physicianSearchSource.value && physicianSearchSource.value !== physicianLocalCacheSourceValue
                ? physicianSearchSource.value
                : "");
            const sourceNodeAttr = retrieveSourceNode
              ? ' data-physician-source-node="' + escapeHTML(retrieveSourceNode) + '"'
              : "";
            const retrieveModality = (result.modalities || []).filter(Boolean).join("/");
            const modalityAttr = retrieveModality
              ? ' data-physician-modality="' + escapeHTML(retrieveModality) + '"'
              : "";
            const retrieveAttrs = sourceNodeAttr + modalityAttr;
            let retrieveAction = '<button class="action action-secondary" type="button" data-physician-retrieve="' + escapeHTML(result.study_instance_uid) + '"' + retrieveAttrs + '>Recuperar estudio</button>';
            if (result.retrieve_status === "done" && result.viewer_url) {
              retrieveAction = "";
            } else if (result.source_node_available === false) {
              retrieveAction = '<button class="action action-secondary" type="button" disabled>Origen no disponible</button>';
            } else if (result.retrieve_status === "running" || result.retrieve_status === "queued") {
              retrieveAction = '<button class="action action-secondary" type="button" disabled data-physician-retrieve-button="' + escapeHTML(result.study_instance_uid) + '">' + escapeHTML(retrieveActionLabel(result.retrieve_status, result.retrieve_phase, result.retrieve_progress)) + '</button>';
            } else if (result.retrieve_status === "failed") {
              retrieveAction = '<button class="action action-secondary" type="button" data-physician-retrieve="' + escapeHTML(result.study_instance_uid) + '"' + retrieveAttrs + ' data-physician-retrieve-button="' + escapeHTML(result.study_instance_uid) + '">Reintentar recuperación</button>';
            }
            if (result.retrieve_status !== "done" && result.retrieve_status !== "running" && result.retrieve_status !== "queued" && result.retrieve_status !== "failed") {
              retrieveAction = '<button class="action action-secondary" type="button" data-physician-retrieve="' + escapeHTML(result.study_instance_uid) + '"' + retrieveAttrs + ' data-physician-retrieve-button="' + escapeHTML(result.study_instance_uid) + '">Recuperar estudio</button>';
            }

            return (
              '<article class="result-item" data-physician-study="' + escapeHTML(result.study_instance_uid) + '">' +
                '<div class="meta-grid">' +
                  '<div><span>ID paciente</span><strong>' + escapeHTML(result.patient_id) + '</strong></div>' +
                  '<div><span>ID estudio</span><strong>' + escapeHTML(result.study_instance_uid) + '</strong></div>' +
                  '<div><span>Apellido y nombre</span><strong>' + escapeHTML(patientNameLabel) + '</strong></div>' +
                  '<div><span>Fecha de estudio</span><strong>' + escapeHTML(studyDateLabel) + '</strong></div>' +
                  '<div><span>Descripción del estudio</span><strong>' + escapeHTML(result.study_description || "Estudio sin descripción") + '</strong></div>' +
                  '<div><span>Modalidades</span><strong>' + escapeHTML(formatModalityList(result.modalities || [])) + '</strong></div>' +
                  '<div><span>Prestación en ANDES</span><strong>' + escapeHTML(andesMetadataLabel(result, "andes_prestacion")) + '</strong></div>' +
                  '<div><span>Profesional en ANDES</span><strong>' + escapeHTML(andesMetadataLabel(result, "andes_professional")) + '</strong></div>' +
                  '<div><span>Imágenes</span><strong>' + escapeHTML(imageCountLabel) + '</strong></div>' +
                '</div>' +
                '<div class="chip-row">' +
                  '<div class="' + chipClassForRetrieve(result.retrieve_status) + '" data-physician-retrieve-chip="' + escapeHTML(result.study_instance_uid) + '">' +
                    escapeHTML(labelForRetrieveStatus(result.retrieve_status, result.retrieve_phase, result.retrieve_progress)) +
                  '</div>' +
                  (result.locations || []).map(location => '<div class="chip success">Hospital: ' + escapeHTML(location) + '</div>').join("") +
                  modalities +
                '</div>' +
                '<div class="inline-actions">' +
                  retrieveAction +
                  action +
                  previewAction +
                  shareAction +
                  ohifAction +
                  andesReportAction +
                  downloadAction +
                '</div>' +
              '</article>'
            );
          })
          .join("");
      }

      function restorePatientStudyFocus(studyInstanceUID) {
        if (!studyInstanceUID) {
          return;
        }

        const selector = '[data-patient-study="' + cssEscape(studyInstanceUID) + '"]';
        const studyCard = patientStudyList.querySelector(selector);
        if (!studyCard) {
          return;
        }

        const focusTarget = studyCard.querySelector("[data-patient-viewer], [data-patient-preview], a, button");
        if (focusTarget && typeof focusTarget.focus === "function") {
          focusTarget.focus({ preventScroll: true });
        }
      }

      async function loadPatientStudies(documentNumber, options = {}) {
        activePatientDocument = documentNumber;
        updateFeedbackAccess();
        const silentRefresh = Boolean(options.silentRefresh);
        const restoreStudyUID = options.restoreStudyUID || "";
        const previousScrollX = window.scrollX;
        const previousScrollY = window.scrollY;

        const params = new URLSearchParams();
        if (patientDateFilter.from) params.set("date_from", patientDateFilter.from);
        if (patientDateFilter.to) {
          params.set("date_to", patientDateFilter.to);
        } else if (patientDateFilter.from) {
          params.set("date_to", patientDateFilter.from);
        }
        if (patientFilterModality.value) params.set("modality", patientFilterModality.value);

        if (!silentRefresh) {
          patientStudyList.innerHTML =
            '<div class="empty-state">Cargando estudios...</div>';
        }

        const response = await fetch("/api/patient/studies?" + params.toString(), {
          headers: { Accept: "application/json" }
        });

        if (!response.ok) {
          throw new Error("patient studies request failed");
        }

        const payload = await response.json();
        renderPatientStudies(payload);
        patientAutoRetrieveQueue = [];
        enqueueAutoRetrieveForPatientStudies(payload.studies || []);
        processPatientAutoRetrieveQueue().catch(() => {});
        if (silentRefresh) {
          window.scrollTo(previousScrollX, previousScrollY);
          restorePatientStudyFocus(restoreStudyUID);
        }
        savePortalWorkspaceState("patient");
      }

      async function fetchPatientSearchStatus(requestID) {
        const response = await fetch("/api/patient/search?request_id=" + encodeURIComponent(requestID), {
          headers: { Accept: "application/json" }
        });

        if (!response.ok) {
          throw new Error("patient search status request failed");
        }

        return response.json();
      }

      async function startPatientSearch(documentNumber) {
        const requestPayload = buildPatientSearchPayload(documentNumber);
        const requestKey = patientSearchKeyForPayload(requestPayload);

        if (activePatientSearchRequestID && activePatientSearchKey === requestKey) {
          const current = await fetchPatientSearchStatus(activePatientSearchRequestID);
          if (current.status === "queued" || current.status === "running") {
            renderPatientSyncStatus(current);
            return current;
          }
          activePatientSearchRequestID = "";
          activePatientSearchKey = "";
        }

        const response = await fetch("/api/patient/search", {
          method: "POST",
          headers: {
            Accept: "application/json",
            "Content-Type": "application/json"
          },
          body: JSON.stringify(requestPayload)
        });

        if (!response.ok) {
          throw new Error("patient search request failed");
        }

        const payload = await response.json();
        if (payload?.status === "queued" || payload?.status === "running") {
          activePatientSearchKey = requestKey;
        }
        return payload;
      }

      async function loadPatientSearchStatus(requestID) {
        const payload = await fetchPatientSearchStatus(requestID);
        renderPatientSyncStatus(payload);
        if (payload.status === "done" || payload.status === "failed") {
          activePatientSearchRequestID = "";
          activePatientSearchKey = "";
          if (activePatientDocument) {
            await loadPatientStudies(activePatientDocument);
          }
        }
        return payload;
      }

      function restorePhysicianResultFocus(studyInstanceUID) {
        if (!studyInstanceUID) {
          return;
        }

        const selector = '[data-physician-study="' + cssEscape(studyInstanceUID) + '"]';
        const studyCard = physicianResultList.querySelector(selector);
        if (!studyCard) {
          return;
        }

        const focusTarget = studyCard.querySelector("[data-physician-retrieve], a, button");
        if (focusTarget && typeof focusTarget.focus === "function") {
          focusTarget.focus({ preventScroll: true });
        }
      }

      function updatePatientRetrieveVisual(payload) {
        const studyUID = payload.study_instance_uid || "";
        if (!studyUID) {
          return;
        }

        const chip = patientStudyList.querySelector('[data-patient-status-chip="' + cssEscape(studyUID) + '"]');
        if (chip) {
          chip.className = chipClassForPatientRetrieveState(payload.status);
          chip.setAttribute("data-patient-status-chip", studyUID);
          chip.textContent = labelForPatientRetrieveState(payload.status);
        }
      }

      function updatePhysicianRetrieveVisual(payload) {
        const studyUID = payload.study_instance_uid || "";
        if (!studyUID) {
          return;
        }

        const chip = physicianResultList.querySelector('[data-physician-retrieve-chip="' + cssEscape(studyUID) + '"]');
        if (chip) {
          chip.className = chipClassForRetrieve(payload.status);
          chip.setAttribute("data-physician-retrieve-chip", studyUID);
          chip.textContent = labelForRetrieveStatus(payload.status, payload.phase, payload.progress);
        }

        const button = physicianResultList.querySelector('[data-physician-retrieve-button="' + cssEscape(studyUID) + '"]');
        if (!button) {
          return;
        }

        button.textContent = retrieveActionLabel(payload.status, payload.phase, payload.progress);
        if (payload.status === "running" || payload.status === "queued" || payload.status === "done") {
          button.disabled = true;
          button.removeAttribute("data-physician-retrieve");
        } else if (payload.status === "failed") {
          button.disabled = false;
          button.setAttribute("data-physician-retrieve", studyUID);
        }
      }

      async function loadPhysicianResults(username, options = {}) {
        activePhysicianUsername = username;
        updateFeedbackAccess();
        const silentRefresh = Boolean(options.silentRefresh);
        const fromAndesAutoRefresh = Boolean(options.fromAndesAutoRefresh);
        const restoreStudyUID = options.restoreStudyUID || "";
        const useInitialCachePeriod = Boolean(options.useInitialCachePeriod);
        const previousScrollX = window.scrollX;
        const previousScrollY = window.scrollY;

        const params = new URLSearchParams();
        if (physicianSearchPatientID.value.trim()) params.set("patient_id", physicianSearchPatientID.value.trim());
        if (physicianSearchPatient.value.trim()) params.set("patient_name", physicianSearchPatient.value.trim());
        if (physicianDateFilter.from) params.set("date_from", physicianDateFilter.from);
        if (physicianDateFilter.to) {
          params.set("date_to", physicianDateFilter.to);
        } else if (physicianDateFilter.from) {
          params.set("date_to", physicianDateFilter.from);
        }
        if (physicianSearchModality.value.trim()) params.set("modality", physicianSearchModality.value.trim());
        params.set("source", physicianSearchSource.value || physicianLocalCacheSourceValue);
        if (useInitialCachePeriod) {
          params.set("use_initial_cache_period", "true");
        }

        if (!silentRefresh) {
          physicianResultList.innerHTML =
            '<div class="empty-state">Buscando estudios...</div>';
        }

        const [resultsResponse, healthPayload] = await Promise.all([
          fetch("/api/physician/results?" + params.toString(), {
            headers: { Accept: "application/json" }
          }),
          fetchDetailedHealth().catch(() => ({ components: [] }))
        ]);

        if (!resultsResponse.ok) {
          const errorPayload = await resultsResponse.json().catch(() => ({}));
          const error = new Error(errorPayload.message || "physician results request failed");
          error.status = resultsResponse.status;
          error.payload = errorPayload;
          throw error;
        }

        const payload = await resultsResponse.json();
        renderPhysicianPACSHealth(healthPayload.components || []);
        if ((payload.filters?.source || physicianLocalCacheSourceValue) !== physicianSearchSource.value) {
          physicianSearchSource.value = payload.filters?.source || physicianLocalCacheSourceValue;
        }
        renderPhysicianResults(payload);
        if (silentRefresh) {
          window.scrollTo(previousScrollX, previousScrollY);
          restorePhysicianResultFocus(restoreStudyUID);
        }
        savePortalWorkspaceState("physician");
        if (!fromAndesAutoRefresh) {
          schedulePhysicianAndesRefresh();
        }
      }

      async function triggerPatientRetrieve(studyInstanceUID, modality) {
        const body = { study_instance_uid: studyInstanceUID };
        if (modality) {
          body.modality = modality;
        }
        const response = await fetch("/api/patient/retrieve", {
          method: "POST",
          headers: {
            Accept: "application/json",
            "Content-Type": "application/json"
          },
          body: JSON.stringify(body)
        });

        if (!response.ok) {
          throw new Error("patient retrieve request failed");
        }

        return response.json();
      }

      async function createPatientStudyShare(studyInstanceUID, viewerKind = "stone", channel = "share") {
        const response = await fetch("/api/patient/studies/" + encodeURIComponent(studyInstanceUID) + "/share", {
          method: "POST",
          headers: {
            Accept: "application/json",
            "Content-Type": "application/json"
          },
          body: JSON.stringify({
            viewer: viewerKind,
            channel
          })
        });

        if (!response.ok) {
          const error = new Error("patient share request failed");
          error.payload = await response.json().catch(() => ({}));
          throw error;
        }

        return response.json();
      }

      async function createPhysicianStudyShare(studyInstanceUID, viewerKind = "stone", channel = "share") {
        const response = await fetch("/api/physician/studies/" + encodeURIComponent(studyInstanceUID) + "/share", {
          method: "POST",
          headers: {
            Accept: "application/json",
            "Content-Type": "application/json"
          },
          body: JSON.stringify({
            viewer: viewerKind,
            channel
          })
        });

        if (!response.ok) {
          const error = new Error("physician share request failed");
          error.payload = await response.json().catch(() => ({}));
          throw error;
        }

        return response.json();
      }

      async function loadPatientStudyPreview(studyInstanceUID) {
        const response = await fetch("/api/patient/studies/" + encodeURIComponent(studyInstanceUID) + "/preview", {
          headers: {
            Accept: "application/json"
          }
        });

        if (!response.ok) {
          const error = new Error("patient preview request failed");
          error.payload = await response.json().catch(() => ({}));
          throw error;
        }

        return response.json();
      }

      async function loadPhysicianStudyPreview(studyInstanceUID) {
        const response = await fetch("/api/physician/studies/" + encodeURIComponent(studyInstanceUID) + "/preview", {
          headers: {
            Accept: "application/json"
          }
        });

        if (!response.ok) {
          const error = new Error("physician preview request failed");
          error.payload = await response.json().catch(() => ({}));
          throw error;
        }

        return response.json();
      }

      async function sendPatientMailCode(documentNumber) {
        const response = await fetch("/api/patient/send-code", {
          method: "POST",
          headers: {
            Accept: "application/json",
            "Content-Type": "application/json"
          },
          body: JSON.stringify({
            document_number: documentNumber
          })
        });

        const payload = await response.json().catch(() => ({}));
        if (!response.ok) {
          const error = new Error(payload.message || "patient send code request failed");
          error.status = response.status;
          error.payload = payload;
          throw error;
        }

        return payload;
      }

      async function loginPatient(documentNumber, code) {
        const response = await fetch("/api/patient/login", {
          method: "POST",
          headers: {
            Accept: "application/json",
            "Content-Type": "application/json"
          },
          body: JSON.stringify({
            document_number: documentNumber,
            code
          })
        });

        const payload = await response.json().catch(() => ({}));
        if (!response.ok) {
          const error = new Error(payload.message || "patient login request failed");
          error.status = response.status;
          error.payload = payload;
          throw error;
        }

        return payload;
      }

      async function fetchViewerAccessURL(role, studyInstanceUID, viewerKind) {
        const basePath = role === "patient" ? "/api/patient/studies/" : "/api/physician/studies/";
        const response = await fetch(basePath + encodeURIComponent(studyInstanceUID) + "/access?viewer=" + encodeURIComponent(viewerKind), {
          headers: {
            Accept: "application/json"
          }
        });

        const payload = await response.json().catch(() => ({}));
        if (!response.ok || !payload?.url) {
          const error = new Error(payload?.message || "viewer access request failed");
          error.status = response.status;
          error.payload = payload;
          throw error;
        }

        return payload.url;
      }

      async function openViewer(role, studyInstanceUID, viewerKind) {
        const viewerTab = window.open("about:blank", "_blank");
        if (viewerTab) {
          try {
            viewerTab.opener = null;
          } catch (_error) {
          }
        }
        if (!viewerTab) {
          throw new Error("viewer popup blocked");
        }
        try {
          const accessURL = await fetchViewerAccessURL(role, studyInstanceUID, viewerKind);
          viewerTab.location.replace(accessURL);
        } catch (error) {
          viewerTab.close();
          throw error;
        }
      }

      async function triggerPhysicianRetrieve(studyInstanceUID, sourceNodeId, modality) {
        const body = { study_instance_uid: studyInstanceUID };
        if (sourceNodeId) {
          body.source_node_id = sourceNodeId;
        }
        if (modality) {
          body.modality = modality;
        }
        const response = await fetch("/api/physician/retrieve", {
          method: "POST",
          headers: {
            Accept: "application/json",
            "Content-Type": "application/json"
          },
          body: JSON.stringify(body)
        });

        if (!response.ok) {
          throw new Error("physician retrieve request failed");
        }

        return response.json();
      }

      async function loginPhysician(username, password) {
        const response = await fetch("/api/physician/login", {
          method: "POST",
          headers: {
            Accept: "application/json",
            "Content-Type": "application/json"
          },
          body: JSON.stringify({
            username,
            password
          })
        });

        const payload = await response.json().catch(() => ({}));
        if (!response.ok) {
          const error = new Error(payload.message || "physician login request failed");
          error.status = response.status;
          error.payload = payload;
          throw error;
        }

        return payload;
      }

      function watchPatientRetrieveJob(jobID, studyInstanceUID) {
        clearPatientRetrievePoll();
        patientRetrieveEventSource = new EventSource("/api/retrieve/jobs/" + encodeURIComponent(jobID) + "/events");
        patientRetrieveEventSource.addEventListener("status", async event => {
          const payload = JSON.parse(event.data);
          updatePatientRetrieveVisual(payload);
          if (payload.status === "done" || payload.status === "failed") {
            clearPatientRetrievePoll();
            patientAutoRetrieveActiveStudyUID = "";
            if (activePatientDocument) {
              await loadPatientStudies(activePatientDocument, {
                silentRefresh: true,
                restoreStudyUID: payload.study_instance_uid || studyInstanceUID || ""
              });
            }
            processPatientAutoRetrieveQueue().catch(() => {});
          }
        });
        patientRetrieveEventSource.onerror = async () => {
          clearPatientRetrievePoll();
          patientAutoRetrieveActiveStudyUID = "";
          if (activePatientDocument) {
            await loadPatientStudies(activePatientDocument, {
              silentRefresh: true,
              restoreStudyUID: studyInstanceUID || ""
            });
          }
          processPatientAutoRetrieveQueue().catch(() => {});
        };
      }

      function watchPhysicianRetrieveJob(jobID, studyInstanceUID) {
        clearPhysicianRetrievePoll();
        physicianRetrieveEventSource = new EventSource("/api/retrieve/jobs/" + encodeURIComponent(jobID) + "/events");
        physicianRetrieveEventSource.addEventListener("status", async event => {
          const payload = JSON.parse(event.data);
          updatePhysicianRetrieveVisual(payload);
          if (payload.status === "done" || payload.status === "failed") {
            clearPhysicianRetrievePoll();
            if (activePhysicianUsername) {
              await loadPhysicianResults(activePhysicianUsername, {
                silentRefresh: true,
                restoreStudyUID: payload.study_instance_uid || studyInstanceUID || ""
              });
            }
          }
        });
        physicianRetrieveEventSource.onerror = async () => {
          clearPhysicianRetrievePoll();
          if (activePhysicianUsername) {
            await loadPhysicianResults(activePhysicianUsername, {
              silentRefresh: true,
              restoreStudyUID: studyInstanceUID || ""
            });
          }
        };
      }

      function connectSystemHealthEvents() {
        if (systemHealthEventSource) {
          systemHealthEventSource.close();
        }

        systemHealthEventSource = new EventSource("/api/system/events");
        systemHealthEventSource.addEventListener("health_status_changed", event => {
          const payload = JSON.parse(event.data);
          if (payload.status === "unavailable") {
            returnToLandingSoft();
            return;
          }
          if (activeWorkspaceKind === "physician") {
            refreshPhysicianPACSHealth();
          }
        });
        systemHealthEventSource.onerror = async () => {
          try {
            const response = await fetch("/api/health", { cache: "no-store" });
            if (response.status === 503) {
              returnToLandingSoft();
            }
          } catch (_error) {
          }
        };
      }

      roleButtons.forEach(button => {
        button.addEventListener("click", () => {
          activateRole(button.dataset.role);
          if (button.dataset.role === "physician") {
            physicianDni.focus({ preventScroll: true });
            return;
          }
          patientDocument.focus({ preventScroll: true });
        });
        button.addEventListener("keydown", event => {
          if (event.key !== "Enter") {
            return;
          }

          event.preventDefault();
          activateRole(button.dataset.role);
          if (button.dataset.role === "physician") {
            physicianDni.focus({ preventScroll: true });
            return;
          }
          patientDocument.focus({ preventScroll: true });
        });
      });

      patientFilterPeriod.addEventListener("change", () => {
        applyPatientPreset(patientFilterPeriod.value);
        savePortalWorkspaceState();
      });

      physicianFilterPeriod.addEventListener("change", () => {
        applyPhysicianPreset(physicianFilterPeriod.value);
        savePortalWorkspaceState();
      });

      patientCalendarPrev.addEventListener("click", () => {
        if (patientDateFilter.viewMonth === 0) {
          patientDateFilter.viewMonth = 11;
          patientDateFilter.viewYear -= 1;
        } else {
          patientDateFilter.viewMonth -= 1;
        }

        renderPatientCalendar();
      });

      patientCalendarNext.addEventListener("click", () => {
        if (patientDateFilter.viewMonth === 11) {
          patientDateFilter.viewMonth = 0;
          patientDateFilter.viewYear += 1;
        } else {
          patientDateFilter.viewMonth += 1;
        }

        renderPatientCalendar();
      });

      patientCalendarGrid.addEventListener("click", event => {
        const button = event.target.closest("[data-patient-calendar-day]");
        if (!button) {
          return;
        }

        selectPatientCalendarDate(button.getAttribute("data-patient-calendar-day"));
        savePortalWorkspaceState();
      });

      patientCalendarGrid.addEventListener("mouseover", event => {
        const button = event.target.closest("[data-patient-calendar-day]");
        if (!button) {
          return;
        }

        previewCalendarRange(patientDateFilter, patientCalendarGrid, patientDateSummary, "data-patient-calendar-day", button.getAttribute("data-patient-calendar-day"));
      });

      patientCalendarGrid.addEventListener("mouseleave", () => {
        clearCalendarPreview(patientCalendarGrid, "data-patient-calendar-day", syncPatientDateSummary);
      });

      physicianCalendarGrid.addEventListener("click", event => {
        const button = event.target.closest("[data-physician-calendar-day]");
        if (!button) {
          return;
        }

        selectPhysicianCalendarDate(button.getAttribute("data-physician-calendar-day"));
        savePortalWorkspaceState();
      });

      physicianCalendarGrid.addEventListener("mouseover", event => {
        const button = event.target.closest("[data-physician-calendar-day]");
        if (!button) {
          return;
        }

        previewCalendarRange(physicianDateFilter, physicianCalendarGrid, physicianDateSummary, "data-physician-calendar-day", button.getAttribute("data-physician-calendar-day"));
      });

      physicianCalendarGrid.addEventListener("mouseleave", () => {
        clearCalendarPreview(physicianCalendarGrid, "data-physician-calendar-day", syncPhysicianDateSummary);
      });

      if (operatorPanelToggle) {
        operatorPanelToggle.addEventListener("click", () => {
          const opening = operatorPanel.hidden;
          operatorPanel.hidden = !opening;
          operatorPanelToggle.setAttribute("aria-pressed", String(opening));
          if (operatorPanel.parentElement) {
            operatorPanel.parentElement.classList.toggle("operator-open", opening);
          }
          operatorPanelToggle.textContent = opening ? "Volver a la búsqueda" : "Métricas y auditoría";
          if (opening) {
            loadOperatorUsage();
          }
        });
      }
      if (operatorRefreshButton) {
        operatorRefreshButton.addEventListener("click", () => loadOperatorUsage());
      }
      if (operatorWindowSelect) {
        operatorWindowSelect.addEventListener("change", () => loadOperatorUsage());
      }
      if (operatorEventsAction) {
        operatorEventsAction.addEventListener("change", () => loadOperatorEvents());
      }
      if (operatorEventsOutcome) {
        operatorEventsOutcome.addEventListener("change", () => loadOperatorEvents());
      }
      if (operatorEventsSearch) {
        let operatorSearchTimer = null;
        operatorEventsSearch.addEventListener("input", () => {
          window.clearTimeout(operatorSearchTimer);
          operatorSearchTimer = window.setTimeout(() => loadOperatorEvents(), 300);
        });
      }
      if (operatorCommentsRefresh) {
        operatorCommentsRefresh.addEventListener("click", () => loadOperatorComments());
      }

      function setFeedbackStatus(text, kind) {
        if (!feedbackStatus) {
          return;
        }
        feedbackStatus.textContent = text || "";
        feedbackStatus.className = "feedback-status" + (kind ? " feedback-status-" + kind : "");
      }

      function openFeedbackDialog() {
        if (!feedbackDialog) {
          return;
        }
        feedbackDialog.hidden = false;
        setFeedbackStatus("");
        if (feedbackMessage) {
          feedbackMessage.value = "";
          window.setTimeout(() => feedbackMessage.focus(), 50);
        }
      }

      function closeFeedbackDialog() {
        if (feedbackDialog) {
          feedbackDialog.hidden = true;
        }
      }

      async function submitFeedback() {
        if (!feedbackMessage) {
          return;
        }
        const message = feedbackMessage.value.trim();
        if (!message) {
          setFeedbackStatus("Escribí un comentario antes de enviar.", "error");
          feedbackMessage.focus();
          return;
        }
        if (feedbackSendButton) {
          feedbackSendButton.disabled = true;
        }
        setFeedbackStatus("Enviando...", "");
        try {
          const response = await fetch("/api/feedback", {
            method: "POST",
            headers: {
              Accept: "application/json",
              "Content-Type": "application/json"
            },
            body: JSON.stringify({ message })
          });
          if (!response.ok) {
            throw new Error("feedback_failed");
          }
          setFeedbackStatus("¡Gracias por tu comentario!", "ok");
          feedbackMessage.value = "";
          window.setTimeout(closeFeedbackDialog, 1200);
        } catch (_error) {
          setFeedbackStatus("No se pudo enviar. Intentá nuevamente.", "error");
        } finally {
          if (feedbackSendButton) {
            feedbackSendButton.disabled = false;
          }
        }
      }

      if (feedbackOpenButton) {
        feedbackOpenButton.addEventListener("click", openFeedbackDialog);
      }
      if (feedbackCloseButton) {
        feedbackCloseButton.addEventListener("click", closeFeedbackDialog);
      }
      if (feedbackDialog) {
        feedbackDialog.addEventListener("click", event => {
          if (event.target === feedbackDialog) {
            closeFeedbackDialog();
          }
        });
      }
      if (feedbackSendButton) {
        feedbackSendButton.addEventListener("click", submitFeedback);
      }
      if (feedbackMessage) {
        feedbackMessage.addEventListener("keydown", event => {
          if ((event.metaKey || event.ctrlKey) && event.key === "Enter") {
            event.preventDefault();
            submitFeedback();
          }
        });
      }
      document.addEventListener("keydown", event => {
        if (event.key === "Escape" && feedbackDialog && !feedbackDialog.hidden) {
          closeFeedbackDialog();
        }
      });

      physicianCalendarPrev.addEventListener("click", () => {
        if (physicianDateFilter.viewMonth === 0) {
          physicianDateFilter.viewMonth = 11;
          physicianDateFilter.viewYear -= 1;
        } else {
          physicianDateFilter.viewMonth -= 1;
        }

        renderPhysicianCalendar();
      });

      physicianCalendarNext.addEventListener("click", () => {
        if (physicianDateFilter.viewMonth === 11) {
          physicianDateFilter.viewMonth = 0;
          physicianDateFilter.viewYear += 1;
        } else {
          physicianDateFilter.viewMonth += 1;
        }

        renderPhysicianCalendar();
      });

      document.addEventListener("click", event => {
        if (!patientDateDropdown.open) {
          return;
        }

        const eventPath = typeof event.composedPath === "function" ? event.composedPath() : [];
        if (eventPath.includes(patientDateDropdown) || patientDateDropdown.contains(event.target)) {
          return;
        }

        if (patientDateFilter.awaitingRangeEnd && patientDateFilter.from && !patientDateFilter.to) {
          patientDateFilter.awaitingRangeEnd = false;
          syncPatientDateSummary();
        }

        patientDateDropdown.open = false;
      });

      document.addEventListener("click", event => {
        if (!physicianDateDropdown.open) {
          return;
        }

        const eventPath = typeof event.composedPath === "function" ? event.composedPath() : [];
        if (eventPath.includes(physicianDateDropdown) || physicianDateDropdown.contains(event.target)) {
          return;
        }

        if (physicianDateFilter.awaitingRangeEnd && physicianDateFilter.from && !physicianDateFilter.to) {
          physicianDateFilter.awaitingRangeEnd = false;
          syncPhysicianDateSummary();
        }

        physicianDateDropdown.open = false;
      });

      resetButtons.forEach(button => {
        button.addEventListener("click", resetLanding);
      });

      patientApplyFiltersButton.addEventListener("click", async () => {
        if (!activePatientDocument) {
          patientStudyList.innerHTML =
            '<div class="empty-state">Ingrese primero por el flujo paciente para cargar su lista.</div>';
          return;
        }

        try {
          const [_, sync] = await Promise.all([
            loadPatientStudies(activePatientDocument),
            startPatientSearch(activePatientDocument)
          ]);
          renderPatientSyncStatus(sync);
        } catch (error) {
          patientStudyList.innerHTML =
            '<div class="empty-state">No se pudieron cargar los estudios del paciente.</div>';
        }
      });

      patientStudyList.addEventListener("click", async event => {
        const viewerButton = event.target.closest("[data-patient-viewer]");
        if (viewerButton) {
          const studyInstanceUID = viewerButton.getAttribute("data-patient-viewer");
          const viewerKind = viewerButton.getAttribute("data-viewer-kind") || "stone";
          if (!studyInstanceUID) {
            return;
          }
          viewerButton.disabled = true;
          try {
            await openViewer("patient", studyInstanceUID, viewerKind);
          } catch (_error) {
            alert("No se pudo abrir el visor para el estudio seleccionado.");
          } finally {
            viewerButton.disabled = false;
          }
          return;
        }

        const shareButton = event.target.closest("[data-patient-share]");
        if (shareButton) {
          const studyInstanceUID = shareButton.getAttribute("data-patient-share");
          const viewerKind = shareButton.getAttribute("data-viewer-kind") || "stone";
          if (!studyInstanceUID) {
            return;
          }

          shareButton.disabled = true;
          const originalLabel = shareButton.textContent;
          shareButton.textContent = "Preparando...";
          try {
            const payload = await createPatientStudyShare(studyInstanceUID, viewerKind, "share");
            if (!payload.qr_code_data_url || !payload.share_url) {
              throw new Error("patient share payload incomplete");
            }
            openPatientShareQR(payload);
          } catch (error) {
            alert(error?.payload?.message || "No se pudo crear el enlace para compartir este estudio.");
          } finally {
            shareButton.disabled = false;
            shareButton.textContent = originalLabel;
          }
          return;
        }

        const previewButton = event.target.closest("[data-patient-preview]");
        if (previewButton) {
          const studyInstanceUID = previewButton.getAttribute("data-patient-preview");
          if (!studyInstanceUID) {
            return;
          }

          previewButton.disabled = true;
          const originalLabel = previewButton.textContent;
          previewButton.textContent = "Cargando...";
          try {
            const payload = await loadPatientStudyPreview(studyInstanceUID);
            openPatientPreview(payload, { shareEnabled: true });
          } catch (error) {
            alert(error?.payload?.message || "No se pudo cargar la vista previa de este estudio.");
          } finally {
            previewButton.disabled = false;
            previewButton.textContent = originalLabel;
          }
          return;
        }

      });

      physicianResultList.addEventListener("click", async event => {
        const viewerButton = event.target.closest("[data-physician-viewer]");
        if (viewerButton) {
          const studyInstanceUID = viewerButton.getAttribute("data-physician-viewer");
          const viewerKind = viewerButton.getAttribute("data-viewer-kind") || "stone";
          if (!studyInstanceUID) {
            return;
          }
          viewerButton.disabled = true;
          try {
            await openViewer("physician", studyInstanceUID, viewerKind);
          } catch (_error) {
            alert("No se pudo abrir el visor para el estudio seleccionado.");
          } finally {
            viewerButton.disabled = false;
          }
          return;
        }

        const previewButton = event.target.closest("[data-physician-preview]");
        if (previewButton) {
          const studyInstanceUID = previewButton.getAttribute("data-physician-preview");
          if (!studyInstanceUID) {
            return;
          }
          previewButton.disabled = true;
          const originalLabel = previewButton.textContent;
          previewButton.textContent = "Cargando...";
          try {
            const payload = await loadPhysicianStudyPreview(studyInstanceUID);
            openPatientPreview(payload);
          } catch (error) {
            alert(error?.payload?.message || "No se pudo cargar la vista previa de este estudio.");
          } finally {
            previewButton.disabled = false;
            previewButton.textContent = originalLabel;
          }
          return;
        }

        const shareButton = event.target.closest("[data-physician-share]");
        if (shareButton) {
          const studyInstanceUID = shareButton.getAttribute("data-physician-share");
          const viewerKind = shareButton.getAttribute("data-viewer-kind") || "stone";
          if (!studyInstanceUID) {
            return;
          }
          shareButton.disabled = true;
          const originalLabel = shareButton.textContent;
          shareButton.textContent = "Preparando...";
          try {
            const payload = await createPhysicianStudyShare(studyInstanceUID, viewerKind, "share");
            if (!payload.qr_code_data_url || !payload.share_url) {
              throw new Error("physician share payload incomplete");
            }
            openPatientShareQR(payload);
          } catch (error) {
            alert(error?.payload?.message || "No se pudo crear el enlace para compartir este estudio.");
          } finally {
            shareButton.disabled = false;
            shareButton.textContent = originalLabel;
          }
          return;
        }

        const button = event.target.closest("[data-physician-retrieve]");
        if (!button) {
          return;
        }

        const studyInstanceUID = button.getAttribute("data-physician-retrieve");
        if (!studyInstanceUID || !activePhysicianUsername) {
          return;
        }
        const sourceNodeId = button.getAttribute("data-physician-source-node") || "";
        const modality = button.getAttribute("data-physician-modality") || "";

        button.disabled = true;
        button.textContent = "Recuperando...";

        try {
          const payload = await triggerPhysicianRetrieve(studyInstanceUID, sourceNodeId, modality);
          if (payload?.job_id) {
            watchPhysicianRetrieveJob(payload.job_id, studyInstanceUID);
          }
        } catch (error) {
          button.disabled = false;
          button.textContent = "Recuperar estudio";
        }
      });

      physicianApplyFiltersButton.addEventListener("click", async () => {
        if (!activePhysicianUsername) {
          physicianResultList.innerHTML =
            '<div class="empty-state">Primero debe ingresar como profesional para aplicar filtros.</div>';
          return;
        }
        if ((physicianSearchSource.value || physicianLocalCacheSourceValue) !== physicianLocalCacheSourceValue &&
            !hasPhysicianQueryFilters({
              patient_id: physicianSearchPatientID.value.trim(),
              patient_name: physicianSearchPatient.value.trim(),
              date_from: physicianDateFilter.from,
              date_to: physicianDateFilter.to || physicianDateFilter.from,
              modality: physicianSearchModality.value.trim()
            })) {
          physicianResultList.innerHTML =
            '<div class="empty-state">Seleccione al menos un filtro adicional antes de consultar un PACS remoto.</div>';
          return;
        }

        try {
          await loadPhysicianResults(activePhysicianUsername);
        } catch (error) {
          physicianResultList.innerHTML =
            '<div class="empty-state">' + escapeHTML(error?.payload?.message || "No se pudieron cargar los resultados del profesional.") + '</div>';
        }
      });

      mailCodeButton.addEventListener("click", async () => {
        const documentValue = normalizePatientDocumentInput(patientDocument.value);
        patientDocument.value = documentValue;
        clearMailCodeFeedback();
        clearPatientLoginErrors();

        if (!documentValue) {
          setFieldError(patientDocument, patientDocumentError, "Ingrese su documento antes de solicitar el código por mail.");
          return;
        }
        if (documentValue.length < 7) {
          setFieldError(patientDocument, patientDocumentError, "Ingrese un documento válido para solicitar el código por mail.");
          return;
        }

        mailCodeButton.disabled = true;
        mailCodeButton.textContent = "Validando...";

        try {
          const payload = await sendPatientMailCode(documentValue);
          patientMailCodeReady = true;
          setMailCodeFeedback(payload.message || "Se enviará un código por mail al contacto registrado.");
        } catch (error) {
          patientMailCodeReady = false;
          const fallbackMessage =
            "No se pudo validar el contacto del paciente. Concurra a su Centro de Salud más cercano para actualizar sus datos de contacto.";
          setFieldError(patientDocument, patientDocumentError, error?.payload?.message || fallbackMessage);
        } finally {
          mailCodeButton.disabled = false;
          mailCodeButton.textContent = "Enviar código";
          syncPatientContinueState();
          if (patientMailCodeReady) {
            focusPatientMailCodeInput();
          }
        }
      });

      patientValidateButton.addEventListener("click", async () => {
        const documentValue = normalizePatientDocumentInput(patientDocument.value);
        patientDocument.value = documentValue;
        const mailCodeValue = patientMailCode.value.trim();
        clearMailCodeFeedback();
        clearPatientLoginErrors();

        if (!documentValue) {
          setFieldError(patientDocument, patientDocumentError, "Ingrese su documento antes de continuar.");
          return;
        }
        if (documentValue.length < 7) {
          setFieldError(patientDocument, patientDocumentError, "Ingrese un documento válido para continuar.");
          return;
        }

        if (!mailCodeValue) {
          setFieldError(patientMailCode, patientMailCodeError, "Ingrese el código para continuar.");
          return;
        }

        patientValidateButton.disabled = true;
        patientValidateButton.textContent = "Validando...";

        try {
          await loginPatient(documentValue, mailCodeValue);
        } catch (error) {
          patientValidateButton.textContent = "Continuar";
          syncPatientContinueState();
          setFieldError(patientMailCode, patientMailCodeError, error?.payload?.message || "No se pudo validar el acceso del paciente.");
          return;
        }

        setMailCodeFeedback("Acceso validado. Estamos cargando sus estudios.");

        window.setTimeout(async () => {
          startPortalSession();
          showWorkspace("patient");
          try {
            const [_, sync] = await Promise.all([
              loadPatientStudies(documentValue),
              startPatientSearch(documentValue)
            ]);
            renderPatientSyncStatus(sync);
          } catch (error) {
            patientSummary.textContent = "Paciente " + documentValue;
            patientStudyList.innerHTML =
              '<div class="empty-state">No se pudieron cargar los estudios disponibles.</div>';
          } finally {
            patientValidateButton.textContent = "Continuar";
            syncPatientContinueState();
          }
        }, 700);
      });

      physicianLoginButton.addEventListener("click", async () => {
        const dniValue = normalizePhysicianDocumentInput(physicianDni.value);
        physicianDni.value = dniValue;
        const passwordValue = physicianPassword.value.trim();
        clearPhysicianLoginErrors();

        physicianNote.classList.remove("warning");

        if (!dniValue) {
          setFieldError(physicianDni, physicianDniError, "Ingrese su DNI antes de continuar.");
          return;
        }

        if (!passwordValue) {
          setFieldError(physicianPassword, physicianPasswordError, "Ingrese su contraseña para continuar.");
          return;
        }

        physicianLoginButton.disabled = true;
        physicianLoginButton.textContent = "Validando...";
        physicianNote.textContent = "Validando acceso profesional...";

        try {
          const payload = await loginPhysician(dniValue, passwordValue);
          startPortalSession();
          showWorkspace("physician");
          physicianFullNameValue.textContent = payload.physician?.full_name || "-";
          physicianDniValue.textContent = payload.physician?.dni || dniValue;
          physicianLicenseValue.textContent = payload.physician?.license_number || "-";
          physicianNote.textContent = payload.message || "Acceso validado.";
          setOperatorAccess(Boolean(payload.can_view_metrics));
          try {
            await loadPhysicianResults(dniValue, { useInitialCachePeriod: true });
          } catch (error) {
            physicianResultList.innerHTML =
              '<div class="empty-state">No se pudieron cargar los resultados del profesional.</div>';
          }
        } catch (error) {
          const message = error?.payload?.message || "No se pudo validar el acceso profesional.";
          if (/usuario|dni/i.test(message)) {
            setFieldError(physicianDni, physicianDniError, message);
          }
          if (/contraseñ|password/i.test(message)) {
            setFieldError(physicianPassword, physicianPasswordError, message);
          }
          if (!/usuario|dni|contraseñ|password/i.test(message)) {
            setFieldError(physicianDni, physicianDniError, message);
            setFieldError(physicianPassword, physicianPasswordError, message);
          }
        } finally {
          physicianLoginButton.textContent = "Continuar";
          syncPhysicianContinueState();
        }
      });

      detachNode(patientWorkspace);
      detachNode(physicianWorkspace);
      detachNode(physicianFlow);
      detachNode(patientShareQROverlay);
      detachNode(patientPreviewOverlay);
      detachNode(mailCodeFeedback);
      demoRibbonStates.forEach(({ ribbon }) => detachNode(ribbon));

      activateRole("patient");
      applyPatientPreset("month");
      patientDocument.addEventListener("input", () => {
        patientDocument.value = normalizePatientDocumentInput(patientDocument.value);
        patientMailCodeReady = false;
        clearFieldError(patientDocument, patientDocumentError);
        syncPatientContinueState();
      });
      patientDocument.addEventListener("keydown", event => {
        if (event.key === "Tab" && !event.shiftKey) {
          event.preventDefault();
          mailCodeButton.focus({ preventScroll: true });
          return;
        }

        if (event.key === "Enter") {
          event.preventDefault();
          if (!mailCodeButton.disabled) {
            mailCodeButton.click();
          }
        }
      });
      patientMailCode.addEventListener("input", () => {
        clearFieldError(patientMailCode, patientMailCodeError);
        syncPatientContinueState();
      });
      patientMailCode.addEventListener("keydown", event => {
        if (event.key === "Tab" && !event.shiftKey) {
          event.preventDefault();
          focusPatientContinueButton();
          return;
        }

        if (event.key === "Enter") {
          event.preventDefault();
          if (!patientValidateButton.disabled) {
            patientValidateButton.click();
            return;
          }
          focusPatientContinueButton();
        }
      });
      patientShareQRClose.addEventListener("click", closePatientShareQR);
      patientShareQROverlay.addEventListener("click", event => {
        if (event.target === patientShareQROverlay) {
          closePatientShareQR();
        }
      });
      patientShareQRShare.addEventListener("click", async () => {
        const shareURL = patientShareQRCopy.dataset.shareUrl || "";
        const shareText = "Te comparto mi estudio de diagnóstico por imágenes.";
        let shared = false;
        if (navigator.share) {
          try {
            await navigator.share({
              title: "Te comparto mi estudio de diagnóstico por imágenes",
              text: shareText,
              url: shareURL
            });
            shared = true;
          } catch (_error) {
            shared = false;
          }
        }
        if (!shared && shareURL && await copyTextToClipboard(shareURL)) {
          alert("Enlace copiado al portapapeles.");
          shared = true;
        }
        if (!shared && shareURL) {
          patientShareQRLink.focus({ preventScroll: true });
          patientShareQRLink.select();
        }
      });
      patientShareQRCopy.addEventListener("click", async () => {
        const shareURL = patientShareQRCopy.dataset.shareUrl || "";
        try {
          if (await copyTextToClipboard(shareURL)) {
            alert("Enlace copiado al portapapeles.");
            return;
          }
        } catch (_error) {
        }
        if (shareURL) {
          patientShareQRLink.focus({ preventScroll: true });
          patientShareQRLink.select();
        }
      });
      patientShareQRWhatsApp.addEventListener("click", () => {
        const whatsAppURL = patientShareQRWhatsApp.dataset.shareUrl || "";
        if (!whatsAppURL) {
          return;
        }
        const popup = window.open(whatsAppURL, "_blank", "noopener");
        if (!popup) {
          alert("No se pudo abrir WhatsApp automáticamente.");
        }
      });
      physicianPacsHealthSummary.addEventListener("click", event => {
        event.stopPropagation();
        togglePhysicianPACSHealthSummary();
      });
      physicianPacsHealthSummary.addEventListener("keydown", event => {
        if (event.key === "Enter" || event.key === " ") {
          event.preventDefault();
          togglePhysicianPACSHealthSummary();
          return;
        }
        if (event.key === "Escape") {
          event.preventDefault();
          closePhysicianPACSHealthSummary();
        }
      });
      document.addEventListener("keydown", event => {
        if (event.key === "Escape" && patientShareQROpen) {
          closePatientShareQR();
        }
        if (event.key === "Escape" && patientPreviewOpen) {
          closePatientPreview();
        }
        if (event.key === "Escape") closePhysicianPACSHealthSummary();
      });
      document.addEventListener("pointerdown", event => {
        if (!physicianPacsHealthSummary.contains(event.target)) {
          closePhysicianPACSHealthSummary();
        }
      });
      patientPreviewCloseFooter.addEventListener("click", closePatientPreview);
      patientPreviewShare.addEventListener("click", async () => {
        if (!patientPreviewShareStudyUID) {
          return;
        }

        patientPreviewShare.disabled = true;
        const originalLabel = patientPreviewShare.textContent;
        patientPreviewShare.textContent = "Preparando...";
        try {
          const payload = await createPatientStudyShare(patientPreviewShareStudyUID, "stone", "share");
          if (!payload.qr_code_data_url || !payload.share_url) {
            throw new Error("patient share payload incomplete");
          }
          closePatientPreview();
          openPatientShareQR(payload);
        } catch (error) {
          alert(error?.payload?.message || "No se pudo crear el enlace para compartir este estudio.");
        } finally {
          patientPreviewShare.disabled = false;
          patientPreviewShare.textContent = originalLabel;
        }
      });
      patientPreviewOverlay.addEventListener("click", event => {
        if (event.target === patientPreviewOverlay) {
          closePatientPreview();
        }
      });
      physicianDni.addEventListener("input", () => {
        physicianDni.value = normalizePhysicianDocumentInput(physicianDni.value);
        clearFieldError(physicianDni, physicianDniError);
        syncPhysicianContinueState();
      });
      physicianDni.addEventListener("keydown", event => {
        if ((event.key === "Tab" && !event.shiftKey) || event.key === "Enter") {
          event.preventDefault();
          focusPhysicianPasswordInput();
        }
      });
      physicianPassword.addEventListener("input", () => {
        clearFieldError(physicianPassword, physicianPasswordError);
        syncPhysicianContinueState();
      });
      physicianPassword.addEventListener("keydown", event => {
        if (event.key === "Tab" && !event.shiftKey) {
          event.preventDefault();
          focusPhysicianContinueButton();
          return;
        }

        if (event.key === "Enter") {
          event.preventDefault();
          if (!physicianLoginButton.disabled) {
            physicianLoginButton.click();
            return;
          }
          focusPhysicianContinueButton();
        }
      });
      physicianSearchPatientID.addEventListener("input", () => {
        physicianSearchPatientID.value = normalizePatientLookupIdentifierInput(physicianSearchPatientID.value);
        savePortalWorkspaceState();
      });
      patientFilterModality.addEventListener("change", () => savePortalWorkspaceState());
      physicianSearchPatient.addEventListener("input", () => savePortalWorkspaceState());
      physicianSearchModality.addEventListener("change", () => savePortalWorkspaceState());
      physicianSearchSource.addEventListener("change", () => savePortalWorkspaceState());
      syncPatientContinueState();
      syncPhysicianContinueState();
      renderPhysicianCalendar();
      connectSystemHealthEvents();
      loadPortalRuntimeConfig()
        .catch(() => {})
        .finally(() => {
          restorePortalWorkspaceState().catch(() => {});
        });
      focusActiveRoleButton();

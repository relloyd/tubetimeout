// ---------- Main App Code ----------
// noinspection ExceptionCaughtLocallyJS

document.addEventListener('DOMContentLoaded', () => {
    const saveButton = document.getElementById('save-config-btn');

    // API endpoints – note the use of /groups instead of /groupMACs.
    const UrlGroupAPI = '/groups';
    const UrlUsageAPI = '/usage';
    const UrlTrackerAPI = '/trackerConfig';

    const nanosecondsPerMinute = 1e9 * 60; // 1e9 nanoseconds per second * 60 seconds
    const nanosecondsPerHour = 1e9 * 60 * 60; // ... * 60 mins
    const nanosecondsPerDay = 1e9 * 60 * 60 * 24; // ... * 24 hours

    let groupMACs = []; // device groups
    let groups = [];  // groups will be an array of objects, each with: { name, retention, threshold, startDay, startDuration, currentMode, modeEndTime }
    let usageData = {};
    let availableMACs = [];

    // ---------- Helper functions for AJAX requests ----------
    async function postData(url, data) {
        try {
            const response = await fetch(url, {
                method: "POST",
                headers: { "Content-Type": "application/x-www-form-urlencoded" },
                body: new URLSearchParams(data),
            });
            // document.getElementById("statusMessage").innerText = await response.text();
            showNotification(await response.text(), false);

        } catch (error) {
            // document.getElementById("statusMessage").innerText = "Error: " + error.message;
            showNotification("Error: " + error.message, true);
        }
    }

    async function putData(url, data) {
        const params = new URLSearchParams(data);
        try {
            const response = await fetch(url, {
                method: "PUT",
                headers: { "Content-Type": "application/x-www-form-urlencoded" },
                body: params,
            });
            // document.getElementById("statusMessage").innerText = await response.text();
            showNotification(await response.text(), false);
        } catch (error) {
            // document.getElementById("statusMessage").innerText = "Error: " + error.message;
            showNotification("Error: " + error.message, true);
        }
    }

    /*
    * Converts a Go duration (in nanoseconds) to a human-readable string.
    * @param {number} duration - The duration in nanoseconds.
    * @returns {string} - A human-readable string (e.g., "45 minutes" or "1 hour 15 minutes").
    */
    function humaniseDuration(duration) {
        // Define conversion constants
        const nsInSecond = 1e9;                   // 1 second = 1e9 nanoseconds
        const nsInMinute = 60 * nsInSecond;         // 1 minute = 60 seconds
        const nsInHour = 60 * nsInMinute;           // 1 hour = 60 minutes
        const nsInDay = 24 * nsInHour;              // 1 day = 24 hours

        if (duration < nsInHour) {
            // For durations less than one hour, show minutes only.
            const minutes = Math.floor(duration / nsInMinute);
            if (minutes > 0) {
                return `${minutes} minute${minutes === 1 ? "" : "s"}`;
            } else {
                return ""
            }
        } else if (duration < nsInDay) {
            // For durations less than one day, show hours and minutes.
            const hours = Math.floor(duration / nsInHour);
            const minutes = Math.floor((duration % nsInHour) / nsInMinute);
            let result = `${hours} hour${hours !== 1 ? "s" : ""}`;
            if (minutes > 0) {
                result += ` ${minutes} minute${minutes !== 1 ? "s" : ""}`;
            }
            return result;
        } else {
            // For durations of one day or more, show days, hours, and minutes.
            const days = Math.floor(duration / nsInDay);
            const remainder = duration % nsInDay;
            const hours = Math.floor(remainder / nsInHour);
            const minutes = Math.floor((remainder % nsInHour) / nsInMinute);

            let result = `${days} day${days !== 1 ? "s" : ""}`;
            if (hours > 0) {
                result += ` ${hours} hour${hours !== 1 ? "s" : ""}`;
            }
            if (minutes > 0) {
                result += ` ${minutes} minute${minutes !== 1 ? "s" : ""}`;
            }
            return result;
        }
    }

    /*
     * Converts a Go duration (in nanoseconds) to a "HH:mm:ss" formatted string.
     * @param {number} duration - The Go duration in nanoseconds.
     * @returns {string} A string formatted as "HH:mm:ss".
     */
    function durationToTimeString(duration) {
        // Convert the duration from nanoseconds to total seconds.
        const totalSeconds = Math.floor(duration / 1e9);

        // Calculate hours, minutes, and seconds.
        const hours = Math.floor(totalSeconds / 3600);
        const minutes = Math.floor((totalSeconds % 3600) / 60);
        const seconds = totalSeconds % 60;

        // Helper function to pad numbers with a leading zero if needed.
        const pad = (num) => String(num).padStart(2, '0');

        const retval = `${pad(hours)}:${pad(minutes)}:${pad(seconds)}`;
        return retval;
    }

    /*
     * Converts a time string to a Golang duration as a string.
     * @param {time} string - The time in HH:mm
     * @returns {string} A string
     */
    function timeStringToDuration(timeString) {
        const [hours, mins] = timeString.split(':').map(Number);
        const minutes = hours * 60 + mins;
        const retval = minutesToDuration(minutes);
        return retval;
    }

    function daysToDuration(days) {
        return nanosecondsPerDay * days;
    }

    function hoursToDuration(hours) {
        return nanosecondsPerHour * hours;
    }

    function minutesToDuration(minutes) {
        return minutes * nanosecondsPerMinute;
    }

    function durationToMinutes(duration) {
        return duration / nanosecondsPerMinute;
    }

    function durationToHours(duration) {
        return duration / nanosecondsPerHour;
    }

    function durationToDays(duration) {
        return duration / nanosecondsPerDay;
    }

    function formatTimeSince(timestampString) {
        const timestamp = new Date(timestampString);
        const now = new Date();
        const diffSeconds = Math.floor((now - timestamp) / 1000);
        if (diffSeconds < 60) return `${diffSeconds} second${diffSeconds === 1 ? '' : 's'} ago`;
        const diffMinutes = Math.floor(diffSeconds / 60);
        if (diffMinutes < 60) return `${diffMinutes} minute${diffMinutes === 1 ? '' : 's'} ago`;
        const diffHours = Math.floor(diffMinutes / 60);
        if (diffHours < 24) return `${diffHours} hour${diffHours === 1 ? '' : 's'} ago`;
        const diffDays = Math.floor(diffHours / 24);
        return `${diffDays} day${diffDays === 1 ? '' : 's'} ago`;
    }

    function formatMinutes(totalMinutes) {
        const hours = Math.floor(totalMinutes / 60);
        const minutes = totalMinutes % 60;
        return `${hours.toString().padStart(2, '0')}:${minutes.toString().padStart(2, '0')}`;
    }

    const modeMonitor = "Monitor";
    const modeBlock = "Block";
    const modeAllow = "Allow";

    // Fetch the device assignments and usage data.
    async function fetchConfigAndRender() {
        await fetchTrackerConfig();
        await fetchUsageData();

        const response = await fetch(UrlGroupAPI);
        groupMACs = await response.json();
        const deviceGroupNames = [...new Set(groupMACs.map(entry => entry.group).filter(Boolean))];
        mergeDeviceGroups(deviceGroupNames);

        renderDevices();
        renderGroups();
        updateGroupSelect();
        updateDeviceGroupDropdown();

        updateAllGroupModes();
    }

    // Fetch tracker configuration – ensure data is an array.
    async function fetchTrackerConfig() {
        try {
            const response = await fetch(UrlTrackerAPI);
            if (response.ok) {
                const configData = await response.json();
                groups = Array.isArray(configData) ? configData : Object.values(configData);
            } else {
                console.error("Failed to fetch tracker config.");
            }
        } catch (error) {
            console.error("Error fetching tracker config:", error);
        }
    }

    // Merge any group names found in device assignments into our groups array.
    function mergeDeviceGroups(deviceGroupNames) {
        deviceGroupNames.forEach(name => {
            if (!groups.find(g => g.name === name)) {
                groups.push({ name: name, retention: 0, threshold: 0, startDay: 0, startDuration: 0, currentMode: modeMonitor, modeEndTime: new Date() });
            }
        });
    }

    // Save both device assignments and tracker configuration.
    async function saveConfig() {
        try {
            // Save Device Groups.
            const deviceGroupsToSave = groupMACs.filter(entry => entry.group);
            let response = await fetch(UrlGroupAPI, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(deviceGroupsToSave),
            });
            if (!response.ok) {
                throw new Error('Failed to save device groups');
            }
            // Save Tracker Config.
            const groupBody = JSON.stringify(groups)
            response = await fetch(UrlTrackerAPI, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: groupBody,
            });
            if (response.ok) {
                showNotification('Configuration saved successfully', false);
            } else {
                showNotification('Failed to save tracker config', true);
            }
        } catch (error) {
            showNotification('Error saving configuration: ' + error.message, true);
        }
        hideSaveButton();
    }

    async function fetchUsageData() {
        try {
            const response = await fetch(UrlUsageAPI);
            usageData = await response.json();
        } catch (error) {
            console.error('Error fetching usage data:', error);
            usageData = {};
        }
    }

    // For each group, call GET /mode?group=<groupName> and update the group's mode info.
    async function updateAllGroupModes() {
        await Promise.all(
            groups.map(async (group) => {
                try {
                    const response = await fetch(`/mode?group=${encodeURIComponent(group.name)}`);
                    if (response.ok) {
                        const data = await response.json();
                        // Convert mode to a number in case it's returned as a string.
                        const modeVal = Number(data.mode);
                        if (modeVal === 0) {
                            group.currentMode = modeMonitor;
                            group.modeEndTime = null;
                        } else if (modeVal === 1) {
                            group.currentMode = modeAllow;
                            group.modeEndTime = new Date(data.modeEndTime);
                        } else if (modeVal === 2) {
                            group.currentMode = modeBlock;
                            group.modeEndTime = new Date(data.modeEndTime);
                        } else {
                            group.currentMode = "unknown";
                            group.modeEndTime = null;
                        }
                    } else {
                        group.currentMode = modeMonitor;
                        group.modeEndTime = null;
                    }
                } catch (e) {
                    console.error(`Error fetching mode for group ${group.name}:`, e);
                    group.currentMode = modeMonitor;
                    group.modeEndTime = null;
                }
            })
        );
        // Re-render groups once all mode data has been updated.
        renderGroups();
    }

    // Render Devices dropdown (for device assignment)
    function renderDevices() {
        const deviceDropdown = document.getElementById('device-dropdown');
        deviceDropdown.innerHTML = '';
        availableMACs = groupMACs;
        availableMACs.forEach(({ mac, name, group }) => {
            const option = document.createElement('option');
            option.value = mac;
            const label = name ? `${mac} - ${name}` : mac;
            option.textContent = group ? `${label} (in ${group})` : label;
            deviceDropdown.appendChild(option);
        });
        deviceDropdown.onchange = updateDeviceNameInput;
        updateDeviceNameInput();
    }

    function updateDeviceNameInput() {
        const mac = document.getElementById('device-dropdown').value;
        const nameInput = document.getElementById('device-name');
        const entry = groupMACs.find(entry => entry.mac === mac);
        nameInput.value = entry && entry.name ? entry.name : '';
    }

    // Update the dropdown used for device assignment.
    function updateDeviceGroupDropdown() {
        const dropdown = document.getElementById('device-group-dropdown');
        dropdown.innerHTML = '';
        groups.forEach(groupObj => {
            const option = document.createElement('option');
            option.value = groupObj.name;
            option.textContent = groupObj.name;
            dropdown.appendChild(option);
        });
    }

    // Render the groups, their devices, tracker configuration, mode status, and per‑group mode controls.
    function renderGroups() {
        const groupsContainer = document.getElementById('groups-container');
        groupsContainer.innerHTML = '';

        // Group the device assignments.
        const grouped = groupMACs.reduce((acc, { group, mac, name }) => {
            if (group) {
                if (!acc[group]) acc[group] = [];
                acc[group].push({ mac, name });
            }
            return acc;
        }, {});

        const sortedGroupNames = Object.keys(grouped).sort();
        sortedGroupNames.forEach(groupName => {
            const groupDiv = document.createElement('div');
            groupDiv.classList.add('group');

            // Group header with title and usage info.
            const groupHeader = document.createElement('div');
            groupHeader.classList.add('group-header');

            const groupTitle = document.createElement('h3');
            groupTitle.textContent = groupName;
            groupHeader.appendChild(groupTitle);

            const usageInfo = document.createElement('span');
            const usage = usageData[groupName] || { used: 0, percentage: 0, activity: {} };
            usageInfo.textContent = `${usage.used} mins (${usage.percentage}%) usage`;
            groupHeader.appendChild(usageInfo);

            // const removeGroupBtn = document.createElement('button');
            // removeGroupBtn.textContent = 'Remove Group';
            // removeGroupBtn.onclick = () => removeGroup(groupName);
            // groupHeader.appendChild(removeGroupBtn);

            groupDiv.appendChild(groupHeader);

            // Display tracker configuration details.
            const groupConfig = groups.find(g => g.name === groupName);
            if (groupConfig) {
                const configInfo = document.createElement('div');
                configInfo.classList.add('group-config-info');
                const retention = humaniseDuration(groupConfig.retention);
                const threshold = humaniseDuration(groupConfig.threshold);
                const startDurationHHMM = formatMinutes(durationToMinutes(groupConfig.startDuration));
                configInfo.textContent = `Block group after ${threshold} usage. Next reset on ${getDayName(groupConfig.startDay)} ${startDurationHHMM}`;
                groupDiv.appendChild(configInfo);
            }

            // Display mode status: show the current mode and its end time.
            const modeStatus = document.createElement('div');
            modeStatus.classList.add('group-mode-status');
            const now = new Date();
            // If the group has a current mode that is not "monitoring" and its end time is in the future...
            if (groupConfig && groupConfig.currentMode !== modeMonitor && groupConfig.modeEndTime && groupConfig.modeEndTime > now) {
                let diffMinutes = Math.round((groupConfig.modeEndTime - now) / 60000);
                if (diffMinutes < 60) {
                    modeStatus.textContent = `${groupConfig.currentMode} for ${diffMinutes} mins`;
                } else {
                    const hours = groupConfig.modeEndTime.getHours().toString().padStart(2, '0');
                    const minutes = groupConfig.modeEndTime.getMinutes().toString().padStart(2, '0');
                    modeStatus.textContent = `${groupConfig.currentMode}ed until ${hours}:${minutes}`;
                }
            } else {
                modeStatus.textContent = ""; // empty text when in normal monitoring mode
            }
            groupDiv.appendChild(modeStatus);

            // List the devices in the group.
            const macList = document.createElement('ul');
            grouped[groupName].forEach(({mac, name}) => {
                const listItem = document.createElement('li');
                const label = document.createElement('span');
                label.textContent = `${mac.replace(/^:/g, '')} - ${name}`;
                label.style.paddingRight = '10px'; // add space before the button
                const lastActiveTimestamp = usage.activity && usage.activity[mac];
                if (lastActiveTimestamp) {
                    const activeTimeSpan = document.createElement('span');
                    activeTimeSpan.classList.add('group-config-info');
                    activeTimeSpan.textContent = ` active ${formatTimeSince(lastActiveTimestamp)}`;
                    label.appendChild(activeTimeSpan);
                }
                const removeBtn = document.createElement('button');
                removeBtn.textContent = 'Remove';
                removeBtn.onclick = () => removeMacFromGroup(mac);
                listItem.appendChild(label);
                listItem.appendChild(removeBtn);
                macList.appendChild(listItem);
            });
            groupDiv.appendChild(macList);

            // ---- Per‑Group Mode Controls ----
            // Wrapper for flex end.
            const modeWrapper = document.createElement('div');
            modeWrapper.classList.add('group-mode-wrapper');

            // Mode select: Allow or Block.
            const modeControls = document.createElement('div');
            modeWrapper.appendChild(modeControls)

            modeControls.classList.add('group-mode-controls');
            modeControls.appendChild(document.createTextNode("Block / Allow: "));
            const modeSelect = document.createElement('select');
            modeSelect.classList.add('group-mode-select');
            const optionAllow = document.createElement('option');
            optionAllow.value = "1";
            optionAllow.textContent = "Allow";
            modeSelect.appendChild(optionAllow);
            const optionBlock = document.createElement('option');
            optionBlock.value = "2";
            optionBlock.textContent = "Block";
            modeSelect.appendChild(optionBlock);
            modeControls.appendChild(modeSelect);

            // Duration select: preset options.
            const durationSelect = document.createElement('select');
            durationSelect.classList.add('group-duration-select');
            const opt15 = document.createElement('option');
            opt15.value = "15";
            opt15.textContent = "15 mins";
            durationSelect.appendChild(opt15);
            const opt60 = document.createElement('option');
            opt60.value = "60";
            opt60.textContent = "1 hour";
            durationSelect.appendChild(opt60);
            const opt120 = document.createElement('option');
            opt120.value = "120";
            opt120.textContent = "2 hours";
            durationSelect.appendChild(opt120);
            const optUntilMidnight = document.createElement('option');
            optUntilMidnight.value = "untilMidnight";
            optUntilMidnight.textContent = "Until Midnight";
            durationSelect.appendChild(optUntilMidnight);
            modeControls.appendChild(durationSelect);

            // "Apply Mode" button.
            const applyModeButton = document.createElement('button');
            applyModeButton.textContent = "Apply";
            applyModeButton.onclick = () => {
                let durationVal = durationSelect.value;
                if (durationVal === "untilMidnight") {
                    const now = new Date();
                    const minutesSinceMidnight = now.getHours() * 60 + now.getMinutes();
                    durationVal = (24 * 60 - minutesSinceMidnight).toString();
                }
                // Update the group’s mode info immediately.
                const groupObj = groups.find(g => g.name === groupName);
                if (groupObj) {
                    groupObj.currentMode = modeSelect.value === "1" ? modeAllow : modeBlock;
                    groupObj.modeEndTime = new Date(Date.now() + parseInt(durationVal, 10) * 60000);
                }
                renderGroups();
                putData("/mode", { group: groupName, minutes: durationVal, mode: modeSelect.value });
            };
            modeControls.appendChild(applyModeButton);

            // "Resume" button sends a DELETE to /mode.
            const resumeModeButton = document.createElement('button');
            resumeModeButton.textContent = "Resume";
            resumeModeButton.onclick = () => {
                fetch(`/mode?group=${encodeURIComponent(groupName)}`, { method: 'DELETE' })
                    .then(response => {
                        if (!response.ok) {
                            throw new Error("Failed to resume mode");
                        }
                        return response.text();
                    })
                    .then(text => {
                        showNotification(text, false);
                        // Update the group's mode information to reflect that it's now monitoring.
                        const groupObj = groups.find(g => g.name === groupName);
                        if (groupObj) {
                            groupObj.currentMode = modeMonitor;
                            groupObj.modeEndTime = null;
                        }
                        renderGroups(); // Re-render groups to update the UI.
                    })
                    .catch(error => {
                        showNotification( "Error: " + error.message, true)
                    });
            };
            modeControls.appendChild(resumeModeButton);

            // ---------------------------------------

            groupDiv.appendChild(modeWrapper);
            groupsContainer.appendChild(groupDiv);
        });
    }

    function removeMacFromGroup(mac) {
        groupMACs.forEach(entry => {
            if (entry.mac === mac) entry.group = '';
        });
        renderDevices();
        renderGroups();
        showSaveButton();
    }

    function removeGroup(groupName) {
        groupMACs.forEach(entry => {
            if (entry.group === groupName) entry.group = '';
        });
        groups = groups.filter(g => g.name !== groupName);
        updateGroupSelect();
        updateDeviceGroupDropdown();
        renderDevices();
        renderGroups();
        showSaveButton();
    }

    function showNotification(message, isError = false) {
        const notification = document.getElementById('notification');
        notification.textContent = message;

        if (isError) {
            notification.classList.add('error');
            notification.classList.remove('success');
        } else {
            notification.classList.add('success');
            notification.classList.remove('error');
        }

        notification.classList.add('show');
        notification.classList.remove('hidden');

        setTimeout(() => {
            notification.classList.remove('show');
            setTimeout(() => {
                notification.classList.add('hidden'); // Hide after transition
            }, 300); // Matches the transition duration
        }, 3000);
    }

    function showSaveButton() {
        saveButton.style.display = 'inline-block';
    }

    function hideSaveButton() {
        saveButton.style.display = 'none';
    }

    function getDayName(day) {
        const days = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];
        return days[parseInt(day, 10)] || "Unknown";
    }

    // ---------- Consolidated Group Configuration Form Handlers ----------
    function updateGroupSelect() {
        const groupSelect = document.getElementById('group-select');
        groupSelect.innerHTML = '';
        const newOption = document.createElement('option');
        newOption.value = "";
        newOption.textContent = "-- New Tracker --";
        groupSelect.appendChild(newOption);
        groups.forEach(groupObj => {
            const option = document.createElement('option');
            option.value = groupObj.name;
            option.textContent = groupObj.name;
            groupSelect.appendChild(option);
        });
        groupSelect.dispatchEvent(new Event('change'));
    }

    document.getElementById('group-select').addEventListener('change', (e) => {
        const selectedName = e.target.value;
        const nameInput = document.getElementById('group-name');
        const retentionInput = document.getElementById('group-retention');
        const thresholdInput = document.getElementById('group-threshold');
        const startDaySelect = document.getElementById('group-start-day');
        const startTimeInput = document.getElementById('group-start-time');
        if (selectedName === "") { // if we need to be ready for a new group...
            nameInput.value = "";
            nameInput.disabled = false;
            retentionInput.value = "";
            thresholdInput.value = "";
            startDaySelect.value = 0;
            startTimeInput.value = "00:00:00";
        } else { // else we're editing an existing group...
            const group = groups.find(g => g.name === selectedName);
            if (group) {
                nameInput.value = group.name;
                nameInput.disabled = true;
                retentionInput.value = durationToDays(group.retention);
                thresholdInput.value = durationToMinutes(group.threshold);
                startDaySelect.value = group.startDay.toString();
                startTimeInput.value = durationToTimeString(group.startDuration);
            }
        }
    });

    document.getElementById('save-tracker-btn').addEventListener('click', () => {
        const groupSelect = document.getElementById('group-select');
        const selectedName = groupSelect.value;
        const nameInput = document.getElementById('group-name').value.trim();
        const retention = parseInt(document.getElementById('group-retention').value, 10);
        const threshold = parseInt(document.getElementById('group-threshold').value, 10);
        const startDay = parseInt(document.getElementById('group-start-day').value, 10);
        const startTime = document.getElementById('group-start-time').value;
        if (!nameInput || isNaN(retention) || isNaN(threshold) || !startTime) {
            alert("Please fill in all fields.");
            return;
        }
        const retentionDuration = daysToDuration(retention);
        const thresholdDuration = minutesToDuration(threshold);
        const startDuration = timeStringToDuration(startTime);
        if (selectedName === "") { // if we're editing a new group...
            if (!groups.find(g => g.name === nameInput)) {  // if the group doesn't exist in memory...
                // Save the group in memory.
                groups.push({ name: nameInput, retention: retentionDuration, threshold: thresholdDuration, startDay: startDay, startDuration: startDuration, currentMode: modeMonitor, modeEndTime: new Date() });
                updateGroupSelect();
                updateDeviceGroupDropdown();
            } else {
                alert("Tracker already exists!");
            }
        } else { // else we're editing an existing group...
            const group = groups.find(g => g.name === selectedName);
            if (group) { // if the group exists in memory...
                // Update the in-memory copy.
                group.retention = retentionDuration;
                group.threshold = thresholdDuration;
                group.startDay = startDay;
                group.startDuration = startDuration;
                showNotification(`Tracker ${group.name} updated. Please hit save or reload the page to undo!`, false);
            }
        }
        showSaveButton();
        renderGroups();
    });

    // Device assignment: add a device to a group.
    document.getElementById('add-to-group-btn').onclick = () => {
        const mac = document.getElementById('device-dropdown').value;
        const name = document.getElementById('device-name').value.trim();
        const group = document.getElementById('device-group-dropdown').value;
        groupMACs.forEach(entry => {
            if (entry.mac === mac) {
                entry.name = name;
                entry.group = group;
            }
        });
        renderDevices();
        renderGroups();
        showSaveButton();
        showNotification(`Group ${group.name} updated. Please hit save or reload the page to undo!`, false);

    };

    saveButton.addEventListener('click', saveConfig);
    fetchConfigAndRender();
});
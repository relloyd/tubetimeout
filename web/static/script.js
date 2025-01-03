// Function to handle AJAX POST requests
async function postData(url, data) {
    try {
        const response = await fetch(url, {
            method: "POST",
            headers: {
                "Content-Type": "application/x-www-form-urlencoded",
            },
            body: new URLSearchParams(data),
        });
        document.getElementById("statusMessage").innerText = await response.text();
    } catch (error) {
        document.getElementById("statusMessage").innerText = "Error: " + error.message;
    }
}

// Attach event listeners to buttons
document.getElementById("addButton").addEventListener("click", () => {
    postData("/pause", { minutes: "60" });
});

document.getElementById("resetButton").addEventListener("click", () => {
    postData("/reset", {});
});

document.addEventListener('DOMContentLoaded', () => {
    const apiUrl = '/groupMACs';
    const usageApiUrl = '/usageSummary'; // Usage data endpoint
    const saveButton = document.getElementById('save-config-btn'); // Reference to Save Button

    let flatGroupMACs = [];
    let groups = [];
    let availableMACs = [];
    let usageData = {}; // Store usage data

    // Fetch configuration and usage data
    async function fetchConfig() {
        const response = await fetch(apiUrl);
        flatGroupMACs = await response.json();

        // Extract groups dynamically
        groups = [...new Set(flatGroupMACs.map(entry => entry.group).filter(Boolean))];

        // Fetch and display usage data
        await fetchUsageData();
        renderDevices();
        renderGroups();
        updateGroupDropdown(); // Populate groups dropdown
    }

    // Fetch usage data from /sampleSummary
    async function fetchUsageData() {
        try {
            const response = await fetch(usageApiUrl);
            usageData = await response.json(); // Example: { "GroupA": { "used": 50, "total": 100, "percentage": 50 } }
        } catch (error) {
            console.error('Error fetching usage data:', error);
            usageData = {}; // Default to empty data
        }
    }

    // Render Devices Dropdown and Inputs
    function renderDevices() {
        const deviceDropdown = document.getElementById('device-dropdown');
        deviceDropdown.innerHTML = ''; // Clear the dropdown

        // Include all MACs, regardless of group assignment
        availableMACs = flatGroupMACs;

        availableMACs.forEach(({ mac, name, group }) => {
            const option = document.createElement('option');
            option.value = mac;

            // Show name and group status in the dropdown
            const label = name ? `${mac} - ${name}` : mac;
            option.textContent = group ? `${label} (in ${group})` : label; // Add group info if available
            deviceDropdown.appendChild(option);
        });

        deviceDropdown.onchange = updateDeviceNameInput;
        updateDeviceNameInput(); // Update input box with selected name
    }

    // Update name input field based on selection
    function updateDeviceNameInput() {
        const mac = document.getElementById('device-dropdown').value;
        const nameInput = document.getElementById('device-name');
        const entry = flatGroupMACs.find(entry => entry.mac === mac);
        nameInput.value = entry.name || '';
    }

    function updateGroupDropdown() {
        const groupDropdown = document.getElementById('group-dropdown');
        groupDropdown.innerHTML = '';

        groups.forEach(group => {
            const option = document.createElement('option');
            option.value = group;
            option.textContent = group;
            groupDropdown.appendChild(option);
        });
    }

    // Add MAC to Group
    document.getElementById('add-to-group-btn').onclick = () => {
        const mac = document.getElementById('device-dropdown').value;
        const name = document.getElementById('device-name').value.trim();
        const group = document.getElementById('group-dropdown').value;

        flatGroupMACs.forEach(entry => {
            if (entry.mac === mac) {
                entry.name = name;
                entry.group = group;
            }
        });

        // Refresh UI
        renderDevices();
        renderGroups();
        updateGroupDropdown();
        showSaveButton();
    };

    // Add New Group
    document.getElementById('add-group-form').onsubmit = (e) => {
        e.preventDefault();
        const newGroup = document.getElementById('new-group-name').value.trim();
        if (newGroup && !groups.includes(newGroup)) {
            groups.push(newGroup);
            updateGroupDropdown();
        }
        document.getElementById('new-group-name').value = '';
    };

    // Save Configuration
    async function saveConfig() {
        const dataToSave = flatGroupMACs.filter(entry => entry.group);
        try {
            const response = await fetch(apiUrl, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(dataToSave),
            });

            if (response.ok) {
                showNotification('Configuration saved successfully', false);
            } else {
                showNotification('Failed to save configuration', true);
            }
        } catch (error) {
            showNotification('Error saving configuration', true);
        }
        hideSaveButton()
    }

    // Show Notification
    function showNotification(message, isError = false) {
        const notification = document.getElementById('notification');
        notification.textContent = message;
        notification.classList.toggle('error', isError);
        notification.classList.remove('hidden');

        setTimeout(() => {
            notification.classList.add('hidden');
        }, 3000);
    }

    // Render Groups Section
    function renderGroups() {
        const groupsContainer = document.getElementById('groups-container');
        groupsContainer.innerHTML = '';

        const grouped = flatGroupMACs.reduce((acc, { group, mac, name }) => {
            if (group) {
                if (!acc[group]) acc[group] = [];
                acc[group].push({ mac, name });
            }
            return acc;
        }, {});

        Object.keys(grouped).forEach(groupName => {
            const groupDiv = document.createElement('div');
            groupDiv.classList.add('group');

            const groupHeader = document.createElement('div');
            groupHeader.classList.add('group-header');

            // Group Title
            const groupTitle = document.createElement('h3');
            groupTitle.textContent = groupName;

            // Usage Info
            const usageInfo = document.createElement('span');
            const usage = usageData[groupName] || { used: 0, percentage: 0 };
            usageInfo.textContent = `Used: ${usage.used} mins (${usage.percentage}%)`;

            // Remove Group Button
            const removeGroupBtn = document.createElement('button');
            removeGroupBtn.textContent = 'Remove Group';
            removeGroupBtn.classList.add('remove-group-btn');
            removeGroupBtn.onclick = () => removeGroup(groupName);

            groupHeader.appendChild(groupTitle);
            groupHeader.appendChild(usageInfo);
            groupHeader.appendChild(removeGroupBtn);
            groupDiv.appendChild(groupHeader);

            const macList = document.createElement('ul');
            grouped[groupName].forEach(({ mac, name }) => {
                const listItem = document.createElement('li');
                const label = document.createElement('span');
                label.textContent = `${mac.replace(/^:/g, '')} - ${name}`;

                const removeBtn = document.createElement('button');
                removeBtn.textContent = 'Remove';
                removeBtn.onclick = () => removeMacFromGroup(mac);

                listItem.appendChild(label);
                listItem.appendChild(removeBtn);
                macList.appendChild(listItem);
            });

            groupDiv.appendChild(macList);
            groupsContainer.appendChild(groupDiv);
        });
    }

    function removeMacFromGroup(mac) {
        flatGroupMACs.forEach(entry => {
            if (entry.mac === mac) entry.group = '';
        });
        renderDevices();
        renderGroups();
        showSaveButton();
    }

    function removeGroup(groupName) {
        flatGroupMACs.forEach(entry => {
            if (entry.group === groupName) entry.group = '';
        });
        groups = groups.filter(g => g !== groupName);
        updateGroupDropdown();
        renderDevices();
        renderGroups();
        showSaveButton();
    }

    // Show Save Button
    function showSaveButton() {
        saveButton.style.display = 'inline-block';
    }

    // Hide Save Button
    function hideSaveButton() {
        saveButton.style.display = 'none';
    }

    saveButton.addEventListener('click', saveConfig); // Event listener for Save Button
    fetchConfig();
});
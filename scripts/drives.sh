#!/bin/bash
# JBOD SAS Drive Management Script
# Dynamically discovers and manages SAS/SATA drives

if [ "$EUID" -ne 0 ]; then
    exec sudo "$0" "$@"
fi

# Dynamic drive discovery via lsscsi
# Finds all disk devices attached via SCSI/SAS/SATA
discover_drives() {
    # Use lsscsi to find disk devices, extract device names
    lsscsi 2>/dev/null | grep ' disk ' | awk '{print $NF}' | sed 's|/dev/||' | sort
}

# Populate DRIVES variable dynamically
DRIVES=$(discover_drives)
DRIVE_COUNT=$(echo "$DRIVES" | wc -w)

if [ -z "$DRIVES" ]; then
    echo "Error: No drives discovered. Is lsscsi installed?"
    exit 1
fi

REFRESH_INTERVAL=${2:-5}

usage() {
    cat << HELP
Usage: drives [command] [options]

Commands:
  status      Show drive states and temperatures (default)
  json        Output drive info as JSON (for integrations)
  monitor [N] Live monitoring, refresh every N seconds (default: 5)
  spindown    Spin down all drives (enter standby)
  spinup      Spin up all drives
  sync        Flush write caches (run before spindown/poweroff)
  poweroff    Safe shutdown: sync caches, then spin down
  settings    Show power management settings
  help        Show this help message

Examples:
  drives              # Show status
  drives json         # JSON output for sidebar/API
  drives monitor      # Live monitoring (5s refresh)
  drives spindown     # Spin down all drives
HELP
}

check_state() {
    local drive=$1
    output=$(smartctl -i -n standby /dev/$drive 2>&1)
    if echo "$output" | grep -q "NOT READY"; then
        echo "standby"
    else
        echo "active"
    fi
}

get_zpool_info() {
    local device=$1
    local zpool_output=$(zpool status -L 2>/dev/null)
    local pool=""
    local vdev=""
    local current_pool=""
    local current_vdev=""
    
    while IFS= read -r line; do
        if echo "$line" | grep -q "pool:"; then
            current_pool=$(echo "$line" | awk '{print $2}')
            current_vdev=""
        elif echo "$line" | grep -qE "raidz|mirror|stripe"; then
            current_vdev=$(echo "$line" | awk '{print $1}')
        elif echo "$line" | grep -q "${device}"; then
            pool=$current_pool
            vdev=$current_vdev
            break
        fi
    done <<< "$zpool_output"
    
    echo "${pool:-null}:${vdev:-null}"
}

json_output() {
    local first=true
    echo '{"drives":['
    
    for d in $DRIVES; do
        if [ "$first" = true ]; then
            first=false
        else
            echo ','
        fi
        
        # Get state
        output=$(smartctl -i -n standby /dev/$d 2>&1)
        if echo "$output" | grep -q "NOT READY"; then
            state="standby"
            temp="null"
            serial="null"
            luid="null"
        else
            state="active"
            smart_output=$(smartctl -A /dev/$d 2>&1)
            temp=$(echo "$smart_output" | grep "Current Drive Temperature" | awk '{print $4}')
            info_output=$(smartctl -i /dev/$d 2>&1)
            serial=$(echo "$info_output" | grep "Serial number:" | awk '{print $3}')
            luid=$(echo "$info_output" | grep "Logical Unit id:" | awk '{print $4}')
        fi
        
        # Get SCSI address
        scsi_addr=$(lsscsi 2>/dev/null | grep "/dev/$d " | awk '{print $1}' | tr -d '[]')
        
        # Get zpool/vdev info
        zpool_info=$(get_zpool_info "$d")
        pool=$(echo "$zpool_info" | cut -d: -f1)
        vdev=$(echo "$zpool_info" | cut -d: -f2)
        
        # Get model
        model=$(lsblk -d -o MODEL /dev/$d 2>/dev/null | tail -1 | xargs)
        
        # Output JSON object
        printf '{"device":"/dev/%s"' "$d"
        printf ',"state":"%s"' "$state"
        if [ "$temp" != "null" ] && [ -n "$temp" ]; then
            printf ',"temp":%s' "$temp"
        else
            printf ',"temp":null'
        fi
        if [ "$serial" != "null" ] && [ -n "$serial" ]; then
            printf ',"serial":"%s"' "$serial"
        else
            printf ',"serial":null'
        fi
        if [ "$luid" != "null" ] && [ -n "$luid" ]; then
            printf ',"luid":"%s"' "$luid"
        else
            printf ',"luid":null'
        fi
        if [ -n "$scsi_addr" ]; then
            printf ',"scsi_addr":"%s"' "$scsi_addr"
        else
            printf ',"scsi_addr":null'
        fi
        if [ "$pool" != "null" ] && [ -n "$pool" ]; then
            printf ',"zpool":"%s"' "$pool"
        else
            printf ',"zpool":null'
        fi
        if [ "$vdev" != "null" ] && [ -n "$vdev" ]; then
            printf ',"vdev":"%s"' "$vdev"
        else
            printf ',"vdev":null'
        fi
        if [ -n "$model" ]; then
            printf ',"model":"%s"' "$model"
        else
            printf ',"model":null'
        fi
        printf '}'
    done
    
    echo '],'
    
    # Summary
    local active_count=0
    local standby_count=0
    local temps=""
    for d in $DRIVES; do
        s=$(check_state $d)
        if [ "$s" = "active" ]; then
            ((active_count++))
            t=$(smartctl -A /dev/$d 2>&1 | grep "Current Drive Temperature" | awk '{print $4}')
            [ -n "$t" ] && temps="$temps $t"
        else
            ((standby_count++))
        fi
    done
    
    if [ -n "$temps" ]; then
        min=$(echo $temps | tr ' ' '\n' | grep -v '^$' | sort -n | head -1)
        max=$(echo $temps | tr ' ' '\n' | grep -v '^$' | sort -n | tail -1)
        avg=$(echo $temps | tr ' ' '\n' | grep -v '^$' | awk '{sum+=$1} END {printf "%.0f", sum/NR}')
        printf '"summary":{"active":%d,"standby":%d,"temp_min":%s,"temp_max":%s,"temp_avg":%s}' \
            "$active_count" "$standby_count" "$min" "$max" "$avg"
    else
        printf '"summary":{"active":%d,"standby":%d,"temp_min":null,"temp_max":null,"temp_avg":null}' \
            "$active_count" "$standby_count"
    fi
    
    echo '}'
}

status() {
    printf "%-10s %-10s %s\n" "DRIVE" "STATE" "TEMP"
    printf "%s\n" "------------------------------"
    for d in $DRIVES; do
        output=$(smartctl -i -n standby /dev/$d 2>&1)
        if echo "$output" | grep -q "NOT READY"; then
            state="STANDBY"
            temp="-"
        else
            state="ACTIVE"
            temp=$(smartctl -A /dev/$d 2>&1 | grep "Current Drive Temperature" | awk '{print $4}')
            temp="${temp}°C"
        fi
        printf "%-10s %-10s %s\n" "/dev/$d" "$state" "$temp"
    done
}

monitor() {
    local interval=$1
    tput civis 2>/dev/null
    trap 'tput cnorm 2>/dev/null; echo ""; echo "Monitoring stopped."; exit 0' INT TERM
    
    while true; do
        tput clear 2>/dev/null || clear
        echo "=== JBOD Drive Monitor === (Ctrl+C to exit)"
        echo "Refreshing every ${interval}s | $(date '+%Y-%m-%d %H:%M:%S')"
        echo ""
        
        local temps=""
        local active_count=0
        local standby_count=0
        
        printf "%-10s %-10s %-8s %s\n" "DRIVE" "STATE" "TEMP" "STATUS"
        printf "%s\n" "-------------------------------------------"
        
        for d in $DRIVES; do
            output=$(smartctl -i -n standby /dev/$d 2>&1)
            if echo "$output" | grep -q "NOT READY"; then
                state="STANDBY"
                temp="-"
                temp_val=""
                status_icon="💤"
                ((standby_count++))
            else
                state="ACTIVE"
                temp_val=$(smartctl -A /dev/$d 2>&1 | grep "Current Drive Temperature" | awk '{print $4}')
                temp="${temp_val}°C"
                temps="$temps $temp_val"
                ((active_count++))
                
                if [ "$temp_val" -ge 60 ]; then
                    status_icon="🔴 HOT"
                elif [ "$temp_val" -ge 55 ]; then
                    status_icon="🟡 WARM"
                else
                    status_icon="🟢 OK"
                fi
            fi
            printf "%-10s %-10s %-8s %s\n" "/dev/$d" "$state" "$temp" "$status_icon"
        done
        
        echo ""
        echo "-------------------------------------------"
        printf "Active: %d | Standby: %d\n" $active_count $standby_count
        
        if [ -n "$temps" ]; then
            min=$(echo $temps | tr ' ' '\n' | grep -v '^$' | sort -n | head -1)
            max=$(echo $temps | tr ' ' '\n' | grep -v '^$' | sort -n | tail -1)
            avg=$(echo $temps | tr ' ' '\n' | grep -v '^$' | awk '{sum+=$1} END {printf "%.0f", sum/NR}')
            printf "Temps: Min %s°C | Max %s°C | Avg %s°C\n" "$min" "$max" "$avg"
        fi
        
        sleep $interval
    done
}

spindown() {
    echo "Spinning down drives..."
    for d in $DRIVES; do
        sdparm --command=stop /dev/$d >/dev/null 2>&1 &
    done
    wait
    
    local max_attempts=30
    local attempt=0
    local all_stopped=false
    
    while [ $attempt -lt $max_attempts ] && [ "$all_stopped" = "false" ]; do
        sleep 1
        all_stopped=true
        stopped_count=0
        
        for d in $DRIVES; do
            state=$(check_state $d)
            if [ "$state" = "standby" ]; then
                ((stopped_count++))
            else
                all_stopped=false
            fi
        done
        
        printf "\r  Progress: %d/%d drives in standby..." $stopped_count $DRIVE_COUNT
        ((attempt++))
    done
    
    echo ""
    
    if [ "$all_stopped" = "true" ]; then
        echo "All drives in standby."
    else
        echo "Warning: Some drives may not have stopped. Run 'drives status' to check."
    fi
}

spinup() {
    echo "Spinning up drives..."
    for d in $DRIVES; do
        sdparm --command=start /dev/$d >/dev/null 2>&1 &
    done
    wait
    
    local max_attempts=60
    local attempt=0
    local all_active=false
    
    while [ $attempt -lt $max_attempts ] && [ "$all_active" = "false" ]; do
        sleep 1
        all_active=true
        active_count=0
        
        for d in $DRIVES; do
            state=$(check_state $d)
            if [ "$state" = "active" ]; then
                ((active_count++))
            else
                all_active=false
            fi
        done
        
        printf "\r  Progress: %d/%d drives active..." $active_count $DRIVE_COUNT
        ((attempt++))
    done
    
    echo ""
    
    if [ "$all_active" = "true" ]; then
        echo "All drives active."
    else
        echo "Warning: Some drives may not have started. Run 'drives status' to check."
    fi
}

sync_drives() {
    echo "Syncing drive caches..."
    for d in $DRIVES; do
        sdparm --command=sync /dev/$d >/dev/null 2>&1 &
    done
    wait
    echo "All caches flushed."
}

poweroff_drives() {
    echo "=== Safe Power Off Sequence ==="
    echo ""
    echo "Step 1: Exporting zpool (if applicable)..."
    # Find zpool using first discovered drive
    first_drive=$(echo $DRIVES | awk '{print $1}')
    zpool_name=$(zpool status 2>/dev/null | grep -B5 "$first_drive" | head -1 | awk '{print $2}')
    if [ -n "$zpool_name" ]; then
        echo "  Found zpool: $zpool_name"
        read -p "  Export zpool $zpool_name? (y/N): " confirm
        if [ "$confirm" = "y" ] || [ "$confirm" = "Y" ]; then
            zpool export "$zpool_name" && echo "  Zpool exported." || echo "  Warning: Failed to export zpool."
        else
            echo "  Skipping zpool export."
        fi
    else
        echo "  No zpool found on these drives."
    fi
    echo ""
    echo "Step 2: Syncing filesystem..."
    sync
    echo "  Filesystem synced."
    echo ""
    echo "Step 3: Flushing drive caches..."
    sync_drives
    echo ""
    echo "Step 4: Spinning down drives..."
    spindown
    echo ""
    echo "=== Safe to power off JBOD ==="
}

settings() {
    # Use first discovered drive for settings
    first_drive=$(echo $DRIVES | awk '{print $1}')
    state=$(check_state $first_drive)
    if [ "$state" = "standby" ]; then
        echo "Drive $first_drive is in standby. Spinning up to read settings..."
        sdparm --command=start /dev/$first_drive >/dev/null 2>&1
        sleep 3
    fi

    echo "Power settings for first drive ($first_drive):"
    echo ""
    sdparm --page=0x1a /dev/$first_drive 2>/dev/null | grep -E "IDLE_A|IDLE_B|IDLE_C|STANDBY_Y|STANDBY_Z|IACT|IBCT|ICCT|SYCT|SZCT"
    echo ""
    echo "Timer values are in 100ms units (e.g., 9000 = 15 min)"
}

case "${1:-status}" in
    status|"")
        status
        ;;
    json)
        json_output
        ;;
    monitor|watch)
        monitor $REFRESH_INTERVAL
        ;;
    spindown|stop)
        spindown
        ;;
    spinup|start)
        spinup
        ;;
    sync)
        sync_drives
        ;;
    poweroff|shutdown)
        poweroff_drives
        ;;
    settings)
        settings
        ;;
    help|--help|-h)
        usage
        ;;
    *)
        echo "Unknown command: $1"
        usage
        exit 1
        ;;
esac

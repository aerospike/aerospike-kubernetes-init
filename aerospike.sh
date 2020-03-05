#!/bin/bash

# Script to manage all operation in Kubernetes Pod for Aerospike
# Operations supported: PreStop, PostStart (only if security enabled).

while getopts ":s:o:U:P:a:b:h:p:" opt;
do
  case ${opt} in
    s ) security_enabled=${OPTARG} ;;
    o ) operation=${OPTARG} ;;
    U ) user_name=${OPTARG} ;;
    P ) password=${OPTARG} ;;
    a ) admin_username=${OPTARG} ;;
    b ) admin_password=${OPTARG} ;;
    h ) hostname=${OPTARG} ;;
    p ) port=${OPTARG} ;;
    \? ) echo "Usage: cmd [-s] [-o] [-U] [-P] [-a] [-b] [-h] [-p]"
         exit 1 ;;
    : ) echo "Invalid option: $OPTARG requires an argument" 1>&2
         exit 1 ;;
  esac
done
shift $((OPTIND -1))

if [ "$security_enabled" = "true" ]
then
    # PostStart Logic
    # Check and create a sys-admin user
    if [ "$operation" = "post-start" ]
    then
        printf "Operation: postStart\n"

        # Wait for the node to be up
        printf "Waiting for the node to be up ...\n"
        while true
        do
            res=$(asinfo -h $hostname -p $port -U $admin_username -P $admin_password -v edition)
            if [ "$res" = "Aerospike Enterprise Edition" ]
            then
                printf "Node is ready!\n"
                break
            fi
        done

        # Create a sys-admin user
        printf "Creating a new user $user_name with sys-admin privilege ...\n"
        retry=0
        while true
        do
            aql -h $hostname -p $port -U $admin_username -P $admin_password -c "create user $user_name password $password role sys-admin" 2>&1 | grep "OK\|AEROSPIKE_USER_ALREADY_EXISTS"
            retVal=$?
            if [ $retVal == 0 ] || [ $retry == 5 ]
            then
                printf "Done\n"
                break
            fi
            sleep 1
            retry=$((retry+1))
        done
        printf "postStart operation complete!\n"
    fi

    # PreStop Logic (with security enabled)
    # Steps:
    # 1. Check if migrations are not ongoing.
    # 2. Quiesce self node and issue recluster.
    # 3. Check if no transactions hitting this node
    if [ "$operation" = "pre-stop" ]
    then
        printf "Operation: preStop\n"

        # Check if migrations are not ongoing
        printf "Waiting for migrations to be completed ...\n"
        while true
        do
            finished=0;
            for part in $(asadm -h $hostname -p $port -U $user_name -P $password --no-config-file -e "asinfo -v statistics -l" | grep migrate_partitions_remaining | cut -d= -f2)
            do
                if [ $part != 0 ]
                then
                    finished=0
                    break
                fi
                finished=1
            done

            if [ $finished != 1 ]
            then
                sleep 3
            else
                printf "Migrations completed\n"
                break
            fi
        done

        # Quiesce self node and issue recluster
        printf "Quiescing self ...\n"
        while true
        do
            quiesce="false"
            asinfo -h $hostname -p $port -U $user_name -P $password -v 'quiesce:'
            for ns in $(asinfo -h $hostname -p $port -U $user_name -P $password -v 'namespaces' -l)
            do
                pending_quiesce=$(asinfo -h $hostname -p $port -U $user_name -P $password -v namespace/$ns -l | grep pending_quiesce | cut -d= -f2)
                if [ "$pending_quiesce" = "false" ]
                then
                    quiesce="false"
                elif [ "$pending_quiesce" = "true" ]
                then
                    quiesce="true"
                fi
            done
            if [ "$quiesce" = "true" ] || [ -z $ns ]
            then
                break
            fi
        done

        printf "Issuing recluster command ...\n"
        asadm -h $hostname -p $port -U $user_name -P $password -e "asinfo -v 'recluster:'"

        # Check if no transactions on this node
        printf "Waiting for transactions to be 0 on self node ...\n"
        while true
        do
            check="false"
            for ops in $(asinfo -h $hostname -p $port -U $user_name -P $password -v 'throughput:' -l | cut -d, -f2 | grep -v "error-no-data-yet-or-back-too-small\|ops/sec")
            do
                if [ "$ops" != "0.0" ]
                then
                    check="false"
                elif [ "$ops" = "0.0" ]
                then
                    check="true"
                fi
            done
            if [ "$check" = "true" ] || [ -z $ops ]
            then
                break
            fi
            sleep 3
        done
        printf "preStop operation complete!\n"
    fi
elif [ "$security_enabled" = "false" ]
then
    # PreStop Logic
    # Steps:
    # 1. Check if migrations are not ongoing.
    # 2. Quiesce self node and issue recluster.
    # 3. Check if no transactions hitting this node
    if [ "$operation" = "pre-stop" ]
    then
        printf "Operation: preStop\n"

        # Check if migrations are not ongoing
        printf "Waiting for migrations to be completed ...\n"
        while true
        do
            finished=0;
            for part in $(asadm -h $hostname -p $port --no-config-file -e 'asinfo -v statistics -l' | grep migrate_partitions_remaining | cut -d= -f2)
            do
                if [ $part != 0 ]
                then
                    finished=0
                    break
                fi
                finished=1
            done

            if [ $finished != 1 ]
            then
                sleep 3
            else
                printf "Migrations completed\n"
                break
            fi
        done

        # Quiesce self node and issue recluster
        printf "Quiescing self ...\n"
        while true
        do
            quiesce="false"
            asinfo -h $hostname -p $port -v 'quiesce:'
            for ns in $(asinfo -h $hostname -p $port -v 'namespaces' -l)
            do
                pending_quiesce=$(asinfo -h $hostname -p $port -v namespace/$ns -l | grep pending_quiesce | cut -d= -f2)
                if [ "$pending_quiesce" = "false" ]
                then
                    quiesce="false"
                elif [ "$pending_quiesce" = "true" ]
                then
                    quiesce="true"
                fi
            done
            if [ "$quiesce" = "true" ] || [ -z $ns ]
            then
                break
            fi
        done

        printf "Issuing recluster command ...\n"
        asadm -h $hostname -p $port -e "asinfo -v 'recluster:'"

        # Check if no transactions on this node
        printf "Waiting for transactions to be 0 on self node ...\n"
        while true
        do
            check="false"
            for ops in $(asinfo -h $hostname -p $port -v 'throughput:' -l | cut -d, -f2 | grep -v "error-no-data-yet-or-back-too-small\|ops/sec")
            do
                if [ "$ops" != "0.0" ]
                then
                    check="false"
                elif [ "$ops" = "0.0" ]
                then
                    check="true"
                fi
            done
            if [ "$check" = "true" ] || [ -z $ops ]
            then
                break
            fi
            sleep 3
        done
        printf "preStop operation complete!\n"
    fi
fi
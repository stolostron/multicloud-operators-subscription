#!/bin/bash

set -e

Pprof_Host="http://a6a6ec0b3dd724a1aa4f261d028c4b0c-1573494742.us-east-2.elb.amazonaws.com:6060/debug/pprof"

Heap="heap"
Goroutine="goroutine"
CPU="profile"
Block="block"

HeapDir="heap_perf"
GoroutineDir="goroutine_perf"
cpuDir="cpu_perf"
blockDir="block_perf"

cnt=0
interval=10

target_cnt=1

main(){
if [ -z "$1" ]
then
    echo "script will donwload the heap and goroutine profile files every $interval seconds with $target_cnt times"
    echo "you can input the count times like:"
    echo "get_pprof.sh 5"
else
    echo "setting the target_cnt to $1"
    target_cnt=$1
fi

mkdir -p $HeapDir
mkdir -p $GoroutineDir
mkdir -p $cpuDir
mkdir -p $blockDir

while (($cnt < target_cnt ))
do
    ts="$(date +%Y-%m-%d-%H-%M-%S-%s)"
    echo "COUNT $cnt"
    echo
    echo "from $Pprof_Host/$Heap, generated: $HeapDir/$Heap-$ts"
    curl $Pprof_Host/$Heap > "$HeapDir/$Heap-$ts"

    echo "from $Pprof_Host/$Goroutine, generated: $GoroutineDir/$Goroutine-$ts"
    curl $Pprof_Host/$Goroutine > "$GoroutineDir/$Goroutine-$ts"


	echo "from $Pprof_Host/$CPU, generated: $cpuDir/$CPU-$ts"
    curl $Pprof_Host/$CPU > "$cpuDir/$CPU-$ts"

    echo "from $Pprof_Host/$Block, generated: $blockDir/$Block-$ts"
    curl $Pprof_Host/$Block > "$blockDir/$Block-$ts"

    ((cnt++))

    if [ $target_cnt != $cnt ]; then
        sleep $interval;
    fi

    echo
done
}

main $@

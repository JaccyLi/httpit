#!/usr/bin/env bash
#
# FileName:          pressure_web.sh
# Date:              2021-08-27
#

AMD_LOG_PING=/tmp/amd_ping.log
ARM_LOG_PING=/tmp/arm_ping.log
AMD_LOG_JELLY=/tmp/amd_JELLY.log
ARM_LOG_JELLY=/tmp/arm_JELLY.log

URI1=/ping
URI2=/jellyopt

PORT=7784

AMD='172.29.53.77'
ARM='172.29.53.72'

DU=100s

LEVEL=()

## amd64 /ping
for ((i=20; i<=400; i+=20))
do
    #echo $i
    go run main.go -X POST -c ${i} -d ${DU} http://${AMD}:${PORT}${URI1} >> ${AMD_LOG_PING}
done

## arm64 /ping
for ((i=20; i<=400; i+=20))
do
    #echo $i
    go run main.go -X POST -c ${i} -d ${DU} http://${ARM}:${PORT}${URI1} >> ${ARM_LOG_PING}
done

## amd64 /jellyopt
for ((i=10; i<=300; i+=10))
do
    #echo $i
    go run main.go  -X POST -c${i} -d${DU} -f /home/suo.li/test/data.bat -H "Authorization: 5A23DQAAAABpyC1hAAAAAKw1NDU5N2FCJ3vfj+Fz51kt8Mhomd9G" -H "Microfun-Client: ver(1.0);game(jelly#C25#8.6.0.3#adr);device(ANDROID#NTH-AN00#stock#11);system(CN#46000#zh);app(0#1#1)" http://${AMD}:${PORT}${URI2} >> ${AMD_LOG_JELLY}
done

## arm64 /jellyopt
for ((i=10; i<=300; i+=10))
do
    #echo $i
    go run main.go  -X POST -c${i} -d${DU} -f /home/suo.li/test/data.bat -H "Authorization: 5A23DQAAAABpyC1hAAAAAKw1NDU5N2FCJ3vfj+Fz51kt8Mhomd9G" -H "Microfun-Client: ver(1.0);game(jelly#C25#8.6.0.3#adr);device(ANDROID#NTH-AN00#stock#11);system(CN#46000#zh);app(0#1#1)" http://${ARM}:${PORT}${URI2} >> ${ARM_LOG_JELLY}
done

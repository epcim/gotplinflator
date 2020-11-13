#!/bin/bash

## THIS IS JUST AN SILLY PROTOTYPE

set -xe

echo "DEBUG: -----------"
echo "DEBUG: pwd $PWD"
echo "DEBUG: param1 $1"
echo "DEBUG: params $*"
env
set
echo "$(cat $1)"
echo "DEBUG: -----------"


function parseYaml {
  local file=$1
  while read -r line
  do
    local k=${line%%:*}
    local v=${line#*:}
    local t=${v#"${v%%[![:space:]]*}"}  # trim leading space

    if [ "$k" == "url" ]; then url="$t"
    elif [ "$k" == "subPath" ]; then subPath="$t"
    elif [ "$k" == "command" ]; then command="$t"
    elif [ "$k" == "hovno1" ]; then hovno1="$t"
    elif [ "$k" == "hovno2" ]; then hovno2="$t"
    fi

  done <"$file"
}

function aterror() {
  RETVAL=$?
  cd $PWD_DIR
  trap true INT TERM EXIT
  if [ $RETVAL -ne 0 ]; then
     ls -Rl $TMP_DIR
  fi

  # cleanup
  test -d $TMP_DIR && {
    ls -la $TMP_DIR
    test -e $TMP_DIR/../kust-plugin-config-* && {
      cat -n $TMP_DIR/../kust-plugin-config-*;
      /bin/rm -rf $TMP_DIR/../kust-plugin-config-*
    }
    /bin/rm -rf $TMP_DIR
  } #2> /dev/null
}


trap aterror INT TERM

TMP_DIR=$(mktemp -d)
PWD_DIR=$PWD

parseYaml $1

if [ -z "$command" ]; then
  command="kustomize build"
fi

test -z "$DEBUG" && true || set -x

go-getter $url $TMP_DIR/got 2> /dev/null
cd $TMP_DIR/got/$subPath
eval $command

test -z "$DEBUG" && /bin/rm -rf $TMP_DIR || true


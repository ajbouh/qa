#!/bin/bash

# usage:
# record-demo.sh <script_file> <output_path>
set -e

SCRIPT=$1
OUTPUT_PATH=$2

# Title is first line of script, with leading "# " removed.
TITLE=$(head -n 1 $SCRIPT | tail -c +3)
DEMO_SEMAPHORE=$PWD/.tmux-semaphore
DEMO_RCFILE=$PWD/.bashrc
MAX_WAIT=2
HEIGHT=30
WIDTH=120
COMMENT_KEY_DELAY=0.02
COMMENT_SPACE_DELAY=0.18
COMMAND_KEY_DELAY=0.06
LINE_DELAY=1.5

trap '(test -e $DEMO_SEMAPHORE && rm $DEMO_SEMAPHORE); (test -e $DEMO_RCFILE && rm $DEMO_RCFILE)' EXIT

SESSION=$USER

function update_semaphore_token() {
  head -c 20 /dev/urandom | xxd -p > $DEMO_SEMAPHORE
}

function await_semaphore_token() {
  tmux wait-for "$(cat $DEMO_SEMAPHORE)"
}

function start_tmux_session() {
  export DEMO_SEMAPHORE
  cat > $DEMO_RCFILE <<'EOF'
PS1='\e[92m»\e[m $(tmux wait-for -S $(cat $DEMO_SEMAPHORE))'
PS2='  \e[92m…\e[m $(tmux wait-for -S $(cat $DEMO_SEMAPHORE))'
EOF

  update_semaphore_token
  tmux -2 \
      new-session \
      -x $WIDTH \
      -y $HEIGHT \
      -d \
      -s $SESSION \
      asciinema rec \
          --title="$TITLE" \
          --max-wait="$MAX_WAIT" \
          --command="/bin/bash --noprofile --rcfile $DEMO_RCFILE" \
          $OUTPUT_PATH
}

function drive_tmux_session() {
  await_semaphore_token

  while read line; do
    if [ "$line" = "" ]; then
      sleep $LINE_DELAY
      continue
    fi

    eol_key=C-m
    if [ "$line" != "#" ]; then
      word_delay=$COMMAND_KEY_DELAY
      char_delay=$COMMAND_KEY_DELAY
      if [ "${line:0:1}" = "#" ]; then
        word_delay=$COMMENT_SPACE_DELAY
        char_delay=$COMMENT_KEY_DELAY
      fi

      # Comment out to keep the leading "# " for comments and use
      # if [ "${line:0:2}" = "# " ]; then
      #   line=$(echo -n "$line" | tail -c +3)
      #   eol_key=C-c
      # fi

      while IFS='' read -n 1 char; do
        if [ "$char" = ' ' ]; then
          key_delay=$word_delay
        else
          key_delay=$char_delay
        fi

        # For some reason, we need to escape semicolons
        if [ "$char" = ';' ]; then
          char='\;'
        fi
        tmux send-keys -l "$char"
        sleep $key_delay
      done < <(echo -n "$line")
    fi

    update_semaphore_token
    tmux send-keys $eol_key
    await_semaphore_token
  done < <(tail -n +2 $SCRIPT)

  sleep $LINE_DELAY
  tmux send-keys C-d
}

start_tmux_session
drive_tmux_session &

tmux set-window-option -t $SESSION force-width $WIDTH
tmux set-window-option -t $SESSION force-height $HEIGHT
tmux set-window-option -t $SESSION aggressive-resize off
# exec tmux attach-session -r -t $SESSION
exec tmux attach-session -t $SESSION

#!/bin/bash

# usage:
# record-2script.sh <script_file> <output_path>
set -e

SCRIPT=$1
OUTPUT_PATH=$2

# Title is first line of script, with leading "# " removed.
TITLE=$(head -n 1 $SCRIPT | tail -c +3)
DEMO_SEMAPHORE=$PWD/.tmux-semaphore
DEMO_RCFILE=$PWD/.bashrc
MAX_WAIT=2
HEIGHT=30
WIDTH=200
COMMENT_KEY_DELAY=0.02
COMMENT_SPACE_DELAY=0.18
COMMAND_KEY_DELAY=0.06
LINE_DELAY=1.8

trap '(test -e $DEMO_SEMAPHORE && rm $DEMO_SEMAPHORE); (test -e $DEMO_RCFILE && rm $DEMO_RCFILE)' EXIT

SESSION=$USER
NESTED_SESSION=${SESSION}_nested

function update_semaphore_token() {
  head -c 20 /dev/urandom | xxd -p > "${DEMO_SEMAPHORE}$1"
}

function await_semaphore_token() {
  tmux wait-for "$(cat ${DEMO_SEMAPHORE}$1)"
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
      asciinema rec -y \
          --title="$TITLE" \
          --max-wait="$MAX_WAIT" \
          --command="/bin/bash --noprofile --rcfile $DEMO_RCFILE" \
          $OUTPUT_PATH
}

function type_tmux_keys() {
  tmux_target="$1"
  keys="$2"

  tmux select-pane -t "$tmux_target"
  tmux send-keys -t "$tmux_target" "$keys"
}

function type_tmux_line() {
  tmux_target="$1"
  line="$2"

  tmux select-pane -t $tmux_target

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
      tmux send-keys -t "$tmux_target" -l "$char"
      sleep $key_delay
    done < <(echo -n "$line")
  fi

  tmux send-keys -t "$tmux_target" $eol_key
}

function drive_tmux_session() {
  tmux_session=$1
  tmux_script=$2

  has_split=
  while IFS= read line; do
    if [ "$line" = "" ]; then
      sleep $LINE_DELAY
      continue
    fi

    # Figure out which session...
    session_index=$(echo "$line" | cut -d' ' -f1)
    line="$(echo "$line" | cut -d' ' -f2-)"

    if [ "${session_index:0:1}" = "1" ]; then
      if [ -z "$has_split" ]; then
        has_split=1
        update_semaphore_token .1
        tmux split-window -t $NESTED_SESSION -h -p 55 env DEMO_SEMAPHORE=${DEMO_SEMAPHORE}.1 /bin/bash --noprofile --rcfile $DEMO_RCFILE
        await_semaphore_token .1
      fi
    fi

    # Is this an asynchronous line?
    if echo "$session_index" | grep -q -E '^\d+&$'; then
      session_index=${session_index:0:${#session_index} - 1}
      type_tmux_line $tmux_session.$session_index "$line"
      # Or an key line
    elif echo "$session_index" | grep -q -E '^\d+E$'; then
      session_index=${session_index:0:${#session_index} - 1}
      type_tmux_keys $tmux_session.$session_index "$line"
      # Is this a well-formed synchronous line?
    elif echo "$session_index" | grep -q -E '^\d+$'; then
      update_semaphore_token .$session_index
      type_tmux_line $tmux_session.$session_index "$line"
      await_semaphore_token .$session_index

      heredoc_token="$(echo "$line" | grep -E '<<([^ ]+)' | sed -E "s/^.*<<'?([^ ']+).*\$/\\1/")"
      if [ -n "$heredoc_token" ]; then
        while IFS= read heredoc_line; do
          tmux send-keys -t "$tmux_session.$session_index" -l "$heredoc_line"
          update_semaphore_token .$session_index
          tmux send-keys -t "$tmux_session.$session_index" C-m
          await_semaphore_token .$session_index
          if [ "$heredoc_line" == "$heredoc_token" ]; then
            break
          fi
        done
      fi
    else
      echo "Malformed line: $line" >&2
      exit 1
    fi
  done < <(tail -n +2 $tmux_script)

  sleep $LINE_DELAY
  tmux send-keys -t $tmux_session.1 C-d
  tmux send-keys -t $tmux_session.0 C-d
}

start_tmux_session
await_semaphore_token

update_semaphore_token .0
tmux send-keys -l "exec tmux new-session -s $NESTED_SESSION env DEMO_SEMAPHORE=${DEMO_SEMAPHORE}.0 /bin/bash --noprofile --rcfile $DEMO_RCFILE ';' set status off"
tmux send-keys C-m

await_semaphore_token .0

drive_tmux_session $NESTED_SESSION $SCRIPT &

tmux set-window-option -t $SESSION force-width $WIDTH
tmux set-window-option -t $SESSION force-height $HEIGHT
tmux set-window-option -t $SESSION aggressive-resize off

# exec tmux attach-session -r -t $SESSION
exec tmux attach-session -t $SESSION

"""Speaks each line of clips.tsv in the voice sets that DMRHub ships.

Usage: python -I tts.py CLIPS.tsv MODEL_DIR OUT_DIR

MODEL_DIR holds kokoro/ (hexgrad/Kokoro-82M at revision
f3ff3571791e39611d31c381e3a41a3af07b4987: config.json, kokoro-v1_0.pth and
voices/*.pt) and kitten/ (the Hugging Face cache of
KittenML/kitten-tts-mini-0.8). It writes OUT_DIR/VOICE/NAME.wav for each
voice set. Needs the kokoro 0.9.4 and kittentts 0.8.1 packages. Both
models add random noise to their output, so tts.py seeds it for each clip;
the sets that DMRHub ships came from runs without a seed, and
inputs.tar.zst keeps their 8 kHz input.
"""
import os
import sys

import numpy as np
import soundfile as sf

KOKORO = ['bm_lewis']
KITTEN = ['Bruno', 'Hugo', 'Luna']


def seed(torch):
    np.random.seed(1)
    if torch is not None:
        torch.manual_seed(1)


def main():
    clips = [line.split('\t') for line in open(sys.argv[1]).read().splitlines()]
    models, out = sys.argv[2], sys.argv[3]
    os.environ.setdefault('HF_HUB_OFFLINE', '1')

    import torch
    from kokoro import KModel, KPipeline
    torch.set_num_threads(8)
    k = f'{models}/kokoro'
    model = KModel(config=f'{k}/config.json', model=f'{k}/kokoro-v1_0.pth').eval()
    pipes = {}
    for v in KOKORO:
        lang = v[0]
        pipes.setdefault(lang, KPipeline(lang_code=lang, model=model))
        d = f'{out}/kokoro-{v.replace("_", "-")}'
        os.makedirs(d, exist_ok=True)
        for name, text in clips:
            seed(torch)
            audio = [a.audio.numpy() for a in pipes[lang](text, voice=f'{k}/voices/{v}.pt')]
            sf.write(f'{d}/{name}.wav', np.concatenate(audio), 24000, subtype='PCM_16')

    from kittentts import KittenTTS
    m = KittenTTS('KittenML/kitten-tts-mini-0.8', cache_dir=f'{models}/kitten')
    for v in KITTEN:
        d = f'{out}/kitten-mini-{v.lower()}'
        os.makedirs(d, exist_ok=True)
        for name, text in clips:
            seed(None)
            sf.write(f'{d}/{name}.wav', m.model.generate(text, voice=v), 24000, subtype='PCM_16')


if __name__ == '__main__':
    main()

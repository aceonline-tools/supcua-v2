import { describe, expect, it } from 'vitest';
import { scanSource } from './icon-scan';

const bad: Record<string, string> = {
  'plain button': '<button>OK</button>',
  'role single quotes': "<span role='button'>x</span>",
  'role spaces around =': '<div role = "button">x</div>',
  'role unquoted': '<div role=button>x</div>',
  'role expression': "<div role={'button'}>x</div>",
  'role conditional expression': "<div role={interactive ? 'button' : undefined}>x</div>",
  'component with role': '<Foo role="button">x</Foo>',
  'self-closing role': '<div role="button" />',
  '> inside an expression attr': '<button onclick={() => a > b ? go() : stop()}>Go</button>',
  '> inside a quoted attr expression': '<button title="{a > b ? \'x\' : \'y\'}">Go</button>',
  'icon only in one if branch': '<button>{#if a}<Icon name="x" />A{:else}B{/if}</button>',
  'if without else': '<button>{#if a}<Icon name="x" />A{/if}</button>',
  'else-if chain missing an icon': '<button>{#if a}<Icon name="x" />{:else if b}B{:else}<Icon name="y" />{/if}</button>',
  'nested if with an empty branch': '<button>{#if a}{#if b}<Icon name="x" />{:else}-{/if}{:else}<Icon name="y" />{/if}</button>',
  'icon in a comment': '<button><!-- <Icon name="x" /> -->T</button>',
  'icon in text of a sibling': '<div><Icon name="x" /></div><button>T</button>',
  'second of two buttons lacks icon': '<button><Icon name="x" />A</button><button>B</button>',
  'nested spans with role': '<span role="button"><span>t</span></span>',
};

const good: Record<string, string> = {
  'icon first': '<button><Icon name="x" />OK</button>',
  'icon outside the conditional': '<button><Icon name="x" />{#if a}A{/if}</button>',
  'icon in every branch with else': '<button>{#if a}<Icon name="x" />A{:else}<Icon name="y" />B{/if}</button>',
  'icon in every else-if branch': '<button>{#if a}<Icon name="x" />{:else if b}<Icon name="y" />{:else}<Icon name="z" />{/if}</button>',
  'icon inside each': '<button>{#each xs as x}<Icon name="x" />{/each}</button>',
  'dynamic icon name': '<button><Icon name={a ? "x" : "y"} size={12} />T</button>',
  '> in attr with an icon': '<button onclick={() => a > b}><Icon name="x" />Go</button>',
  'non-button elements ignored': '<div role="listbox"><span>x</span></div>',
};

describe('icon scanner regressions', () => {
  for (const [name, src] of Object.entries(bad)) it(`fails: ${name}`, () => expect(scanSource(src).length).toBeGreaterThan(0));
  for (const [name, src] of Object.entries(good)) it(`passes: ${name}`, () => expect(scanSource(src)).toEqual([]));
  it('reports the line of the offending button', () => {
    expect(scanSource('<p>a</p>\n\n<button>B</button>')[0].line).toBe(3);
  });
});
